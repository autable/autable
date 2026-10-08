package ctl

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strconv"
	"time"
)

const loginTimeout = 5 * time.Minute

type userInfo struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Provider    string `json:"provider"`
}

type tokenResponse struct {
	Token     string   `json:"token"`
	ExpiresAt int64    `json:"expires_at"`
	User      userInfo `json:"user"`
}

func randomString() (string, error) {
	data := make([]byte, 32)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

// browserLogin runs the loopback authorization flow and returns the issued
// token.
func (a *app) browserLogin(ctx context.Context, server string, openBrowser bool) (tokenResponse, error) {
	verifier, err := randomString()
	if err != nil {
		return tokenResponse{}, err
	}
	state, err := randomString()
	if err != nil {
		return tokenResponse{}, err
	}
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return tokenResponse{}, err
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port

	type result struct {
		code string
		err  error
	}
	results := make(chan result, 1)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /callback", func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("state") != state {
			http.Error(w, "state mismatch; start autablectl login again", http.StatusBadRequest)
			return
		}
		code := query.Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<!doctype html><meta charset=\"utf-8\"><title>autablectl</title><p>autablectl is signed in. You can close this window.</p>")
		select {
		case results <- result{code: code}:
		default:
		}
	})
	callbackServer := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		if err := callbackServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			select {
			case results <- result{err: err}:
			default:
			}
		}
	}()
	defer callbackServer.Close()

	authorizeURL := server + "/api/auth/cli/authorize?" + url.Values{
		"port":      {strconv.Itoa(port)},
		"state":     {state},
		"challenge": {challenge},
	}.Encode()
	fmt.Fprintf(a.stderr, "Open this URL in a browser on this computer to authorize autablectl:\n\n  %s\n\n", authorizeURL)
	if openBrowser {
		if err := a.openURL(authorizeURL); err != nil {
			fmt.Fprintf(a.stderr, "could not open a browser (%v); open the URL manually\n", err)
		}
	}
	fmt.Fprintln(a.stderr, "Waiting for authorization...")

	ctx, cancel := context.WithTimeout(ctx, loginTimeout)
	defer cancel()
	var got result
	select {
	case got = <-results:
	case <-ctx.Done():
		return tokenResponse{}, errors.New("timed out waiting for browser authorization")
	}
	if got.err != nil {
		return tokenResponse{}, got.err
	}
	anonymous := &client{server: server, http: a.httpClient}
	var token tokenResponse
	err = anonymous.call(http.MethodPost, "/api/auth/cli/token", map[string]string{
		"code":     got.code,
		"verifier": verifier,
	}, &token)
	return token, err
}

func openURLInBrowser(target string) error {
	var command *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		command = exec.Command("rundll32", "url.dll,FileProtocolHandler", target)
	case "darwin":
		command = exec.Command("open", target)
	default:
		command = exec.Command("xdg-open", target)
	}
	return command.Start()
}
