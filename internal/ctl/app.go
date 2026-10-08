// Package ctl implements autablectl, the command-line client for a running
// autable server. Every command prints JSON on stdout so scripts and coding
// agents can consume it; diagnostics go to stderr.
package ctl

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"

	"autable/internal/version"
)

type app struct {
	stdin      io.Reader
	stdout     io.Writer
	stderr     io.Writer
	getenv     func(string) string
	configPath string
	httpClient *http.Client
	openURL    func(string) error
}

type command struct {
	usage   string
	summary string
	run     func(a *app, args []string) error
}

// Main runs autablectl and returns the process exit code.
func Main(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	configPath, err := defaultConfigPath()
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	a := &app{
		stdin:      stdin,
		stdout:     stdout,
		stderr:     stderr,
		getenv:     os.Getenv,
		configPath: configPath,
		httpClient: newHTTPClient(),
		openURL:    openURLInBrowser,
	}
	return a.run(args)
}

func (a *app) run(args []string) int {
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		a.printHelp()
		return 0
	}
	if args[0] == "version" || args[0] == "--version" {
		fmt.Fprintln(a.stdout, version.String())
		return 0
	}
	cmd, rest, ok := lookupCommand(args)
	if !ok {
		fmt.Fprintf(a.stderr, "error: unknown command %q; run autablectl help\n", strings.Join(args[:min(2, len(args))], " "))
		return 2
	}
	if err := cmd.run(a, rest); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		var usage usageError
		if errors.As(err, &usage) {
			fmt.Fprintf(a.stderr, "error: %s\nusage: autablectl %s\n", usage.message, cmd.usage)
			return 2
		}
		fmt.Fprintln(a.stderr, "error:", err)
		return 1
	}
	return 0
}

// lookupCommand matches the longest command name ("rows list" before "rows").
func lookupCommand(args []string) (command, []string, bool) {
	if len(args) >= 2 {
		if cmd, ok := commands[args[0]+" "+args[1]]; ok {
			return cmd, args[2:], true
		}
	}
	cmd, ok := commands[args[0]]
	return cmd, args[1:], ok
}

func (a *app) printHelp() {
	fmt.Fprint(a.stdout, `autablectl — command-line client for an autable server.

Output is JSON on stdout. Sign in once with "autablectl login --server URL";
AUTABLE_SERVER and AUTABLE_TOKEN override the stored login.

JSON arguments (--values, --query, --inputs, ...) accept inline JSON,
@path to read a file, or - to read stdin.

Commands:
`)
	names := make([]string, 0, len(commands))
	for name := range commands {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		cmd := commands[name]
		fmt.Fprintf(a.stdout, "  %-62s %s\n", cmd.usage, cmd.summary)
	}
	fmt.Fprintln(a.stdout, "\nRun \"autablectl <command> -h\" for a command's flags. See docs/cli.md.")
}

type usageError struct{ message string }

func (err usageError) Error() string { return err.message }

func usagef(format string, args ...any) error {
	return usageError{message: fmt.Sprintf(format, args...)}
}

// parseFlags parses flags that may appear before, between, or after the
// positional arguments, and checks the positional count.
func parseFlags(fs *flag.FlagSet, args []string, positional ...string) ([]string, error) {
	fs.SetOutput(io.Discard)
	var values []string
	for {
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return nil, err
			}
			return nil, usagef("%v", err)
		}
		args = fs.Args()
		if len(args) == 0 {
			break
		}
		if args[0] == "--" {
			values = append(values, args[1:]...)
			break
		}
		values = append(values, args[0])
		args = args[1:]
	}
	required := 0
	for _, name := range positional {
		if !strings.HasSuffix(name, "?") {
			required++
		}
	}
	if len(values) < required || len(values) > len(positional) {
		return nil, usagef("expected arguments <%s>, got %d", strings.Join(positional, "> <"), len(values))
	}
	return values, nil
}

func (a *app) printJSON(value any) error {
	encoder := json.NewEncoder(a.stdout)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

// printRaw re-indents a JSON response body.
func (a *app) printRaw(data []byte) error {
	if len(strings.TrimSpace(string(data))) == 0 {
		return a.printJSON(map[string]bool{"ok": true})
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		_, err := a.stdout.Write(data)
		return err
	}
	return a.printJSON(value)
}

// readJSONArg reads inline JSON, @file, or - (stdin) into target.
func (a *app) readJSONArg(name, value string, target any) error {
	data, err := a.readArg(value)
	if err != nil {
		return fmt.Errorf("--%s: %w", name, err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("--%s is not valid JSON: %w", name, err)
	}
	return nil
}

func (a *app) readArg(value string) ([]byte, error) {
	switch {
	case value == "-":
		return io.ReadAll(a.stdin)
	case strings.HasPrefix(value, "@"):
		return os.ReadFile(value[1:])
	default:
		return []byte(value), nil
	}
}

// client returns an authenticated API client from the environment or the
// stored login.
func (a *app) client() (*client, error) {
	creds, _, err := loadCredentials(a.configPath)
	if err != nil {
		return nil, err
	}
	if server := a.getenv("AUTABLE_SERVER"); server != "" {
		if server != creds.Server {
			creds.Token = ""
		}
		creds.Server = server
	}
	if token := a.getenv("AUTABLE_TOKEN"); token != "" {
		creds.Token = token
	}
	if creds.Server == "" || creds.Token == "" {
		return nil, errors.New("not signed in; run: autablectl login --server https://your-autable-server")
	}
	server, err := normalizeServer(creds.Server)
	if err != nil {
		return nil, err
	}
	return &client{server: server, token: creds.Token, http: a.httpClient}, nil
}

func (a *app) context() context.Context {
	return context.Background()
}
