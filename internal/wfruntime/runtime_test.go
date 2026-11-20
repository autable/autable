package wfruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"autable/internal/egress"
)

type fakeHost struct {
	fetches atomic.Int32
	fetch   func(via string, request egress.Request) (egress.Response, error)
	table   func(op, table string, args map[string]any) (any, error)
}

func (host *fakeHost) Fetch(ctx context.Context, via string, request egress.Request) (egress.Response, error) {
	host.fetches.Add(1)
	if host.fetch == nil {
		return egress.Response{}, errors.New("no fetch")
	}
	return host.fetch(via, request)
}

func (host *fakeHost) Table(ctx context.Context, op, table string, args map[string]any) (any, error) {
	if host.table == nil {
		return nil, &HostError{Name: "PermissionError", Err: errors.New("permission denied")}
	}
	return host.table(op, table, args)
}

func (host *fakeHost) Users(ctx context.Context, op string, args map[string]any) (any, error) {
	return map[string]any{"id": args["id"], "email": "u@example.com"}, nil
}

func (host *fakeHost) Files(ctx context.Context, op string, args map[string]any) (any, error) {
	return map[string]any{"size": len(args["body"].([]byte))}, nil
}

// bundle wraps a run function body into a workflow bundle.
func bundle(run string) string {
	return "var __workflow = { triggers: { start: { kind: \"manual\" } }, run: " + run + " };"
}

func runScript(t *testing.T, source string, options Options) (Result, error) {
	t.Helper()
	program, err := Compile("workflow.js", source)
	if err != nil {
		t.Fatal(err)
	}
	options.Program = program
	if options.Timeout == 0 {
		options.Timeout = 5 * time.Second
	}
	if options.Host == nil {
		options.Host = &fakeHost{}
	}
	if options.Event == nil {
		options.Event = map[string]any{"trigger": "start", "runId": 1}
	}
	return Run(context.Background(), options)
}

func mustRun(t *testing.T, source string, options Options) Result {
	t.Helper()
	result, err := runScript(t, source, options)
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	return result
}

func TestRunReturnsJSONOutputAndLogs(t *testing.T) {
	result := mustRun(t, bundle(`async function (event) {
		console.log("hello", { a: 1 });
		__autable_host.log("warn", "careful", { n: 2 });
		const waited = await new Promise((resolve) => setTimeout(resolve, 5, "timer"));
		return { trigger: event.trigger, runId: event.runId, waited, secret: __autable_host.secret("token"), missing: __autable_host.secret("nope") === undefined, v: __autable_host.variable("channel"), info: __autable_host.info };
	}`), Options{Secrets: map[string]string{"token": "s3"}, Vars: map[string]string{"channel": "ops"}, Info: Info{Database: "db", Workflow: "sync", Version: 3, RunID: 1, Trigger: "start"}})
	var output map[string]any
	if err := json.Unmarshal([]byte(result.Output), &output); err != nil {
		t.Fatalf("invalid output %q: %v", result.Output, err)
	}
	info := output["info"].(map[string]any)
	if output["trigger"] != "start" || output["runId"] != 1.0 || output["waited"] != "timer" || output["secret"] != "s3" || output["missing"] != true || output["v"] != "ops" || info["workflow"] != "sync" || info["version"] != 3.0 {
		t.Fatalf("unexpected output %#v", output)
	}
	if len(result.Logs) != 2 || result.Logs[0].Message != `hello {"a":1}` || result.Logs[0].Level != "info" || result.Logs[1].Level != "warn" || result.Logs[1].Data != `{"n":2}` {
		t.Fatalf("unexpected logs %#v", result.Logs)
	}
}

func TestUndefinedOutputIsEmpty(t *testing.T) {
	if result := mustRun(t, bundle(`function () {}`), Options{}); result.Output != "" {
		t.Fatalf("expected no output, got %q", result.Output)
	}
}

func TestHostPromisesSettleOnTheLoop(t *testing.T) {
	host := &fakeHost{
		fetch: func(via string, request egress.Request) (egress.Response, error) {
			return egress.Response{Status: 201, StatusText: "Created", URL: request.URL, Headers: [][2]string{{"content-type", "text/plain"}}, Body: []byte("via=" + via + " body=" + string(request.Body) + " method=" + request.Method)}, nil
		},
		table: func(op, table string, args map[string]any) (any, error) {
			if table == "other" {
				return nil, &HostError{Name: "PermissionError", Err: errors.New("permission denied")}
			}
			return []any{map[string]any{"record_id": int64(7), "values": map[string]any{"名称": table + ":" + op}}}, nil
		},
	}
	result := mustRun(t, bundle(`async function () {
		const [response, rows] = await Promise.all([
			__autable_host.fetch({ url: "https://example.com/x", method: "POST", body: "hi", via: "office", headers: [["x-a", "1"]] }),
			__autable_host.table("list", "contacts", { limit: 1 }),
		]);
		const text = new TextDecoder().decode(response.body);
		let denied = "";
		try { await __autable_host.table("create", "other", {}); } catch (error) { denied = error.name + ": " + error.message; }
		return { status: response.status, statusText: response.statusText, header: response.headers[0][1], text, isBuffer: response.body instanceof ArrayBuffer, rows, isArray: Array.isArray(rows), denied };
	}`), Options{Host: host})
	var output map[string]any
	if err := json.Unmarshal([]byte(result.Output), &output); err != nil {
		t.Fatal(err)
	}
	if output["status"] != 201.0 || output["statusText"] != "Created" || output["header"] != "text/plain" || output["text"] != "via=office body=hi method=POST" || output["isBuffer"] != true || output["isArray"] != true {
		t.Fatalf("unexpected output %#v", output)
	}
	if rows := output["rows"].([]any); rows[0].(map[string]any)["values"].(map[string]any)["名称"] != "contacts:list" {
		t.Fatalf("unexpected rows %#v", output["rows"])
	}
	if output["denied"] != "PermissionError: permission denied" {
		t.Fatalf("expected a named permission error, got %#v", output["denied"])
	}
}

func TestFetchArgumentsAreValidatedBeforeTheHost(t *testing.T) {
	host := &fakeHost{}
	_, err := runScript(t, bundle(`async function () { await __autable_host.fetch({ url: "file:///etc/passwd" }); }`), Options{Host: host})
	if err == nil || !strings.Contains(err.Error(), "must be http or https") || host.fetches.Load() != 0 {
		t.Fatalf("expected a scheme error without a fetch, got %v (%d fetches)", err, host.fetches.Load())
	}
}

func TestRunErrors(t *testing.T) {
	cases := map[string]struct {
		source string
		want   string
	}{
		"throw":           {bundle(`function () { throw new Error("boom"); }`), "boom"},
		"reject":          {bundle(`async function () { await null; throw new TypeError("bad input"); }`), "TypeError: bad input"},
		"never settles":   {bundle(`function () { return new Promise(() => {}); }`), "never settles"},
		"no export":       {`var x = 1;`, "does not define globalThis.__workflow"},
		"run not fn":      {`var __workflow = { run: 1 };`, "not a function"},
		"timer throws":    {bundle(`function () { return new Promise(() => setTimeout(() => { throw new Error("in timer"); }, 1)); }`), "in timer"},
		"output too big":  {bundle(`function () { return "x".repeat(2 << 20); }`), "run output is"},
		"syntax at start": {bundle(`function () { return JSON.parse("{"); }`), "SyntaxError"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := runScript(t, tc.source, Options{})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestTimeoutInterruptsCPUAndWaits(t *testing.T) {
	started := time.Now()
	_, err := runScript(t, bundle(`function () { for (;;) {} }`), Options{Timeout: 200 * time.Millisecond})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("expected a timeout, got %v", err)
	}
	if time.Since(started) > 3*time.Second {
		t.Fatalf("interrupt took %s", time.Since(started))
	}
	_, err = runScript(t, bundle(`function () { return new Promise((resolve) => setTimeout(resolve, 60000)); }`), Options{Timeout: 200 * time.Millisecond})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("expected a timeout while waiting, got %v", err)
	}
	// A blocked host call is cancelled with the run.
	host := &fakeHost{fetch: func(string, egress.Request) (egress.Response, error) {
		time.Sleep(50 * time.Millisecond)
		return egress.Response{Status: 200}, nil
	}}
	result, err := runScript(t, bundle(`async function () { const r = await __autable_host.fetch({ url: "http://x.test/", timeoutMs: 999999 }); return r.status; }`), Options{Host: host, Timeout: 2 * time.Second})
	if err != nil || result.Output != "200" {
		t.Fatalf("expected the fetch to finish within the run, got %q %v", result.Output, err)
	}
}

func TestCancelInterruptsTheRun(t *testing.T) {
	program, err := Compile("workflow.js", bundle(`function () { for (;;) {} }`))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(100*time.Millisecond, cancel)
	_, err = Run(ctx, Options{Program: program, Timeout: time.Minute, Host: &fakeHost{}, Event: map[string]any{}})
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("expected a cancellation, got %v", err)
	}
}

func TestClearTimeoutLetsTheRunFinish(t *testing.T) {
	result := mustRun(t, bundle(`async function () {
		const id = setTimeout(() => { throw new Error("should not fire"); }, 10);
		clearTimeout(id);
		await new Promise((resolve) => setTimeout(resolve, 30));
		return "ok";
	}`), Options{})
	if result.Output != `"ok"` {
		t.Fatalf("unexpected output %q", result.Output)
	}
}

func TestWebhookResponses(t *testing.T) {
	event := map[string]any{"trigger": "hook", "runId": 2, "request": map[string]any{"method": "POST", "body": []byte(`{"ping":1}`), "headers": map[string]any{"x-signature": "abc"}}}
	cases := []struct {
		run         string
		status      int
		contentType string
		body        string
	}{
		{`function (event) { return { status: 200, body: new TextDecoder().decode(event.request.body) }; }`, 200, "text/plain; charset=utf-8", `{"ping":1}`},
		{`function (event) { return { status: 202, body: { header: event.request.headers["x-signature"], isBuffer: event.request.body instanceof ArrayBuffer } }; }`, 202, "application/json", `{"header":"abc","isBuffer":true}`},
		{`function () { return { status: 200, headers: { "Content-Type": "application/xml" }, body: new TextEncoder().encode("<ok/>") }; }`, 200, "application/xml", "<ok/>"},
		{`function () { return { status: 201, body: new Uint8Array([1, 2]).buffer }; }`, 201, "application/octet-stream", "\x01\x02"},
		{`function () {}`, 204, "", ""},
	}
	for index, tc := range cases {
		result, err := runScript(t, bundle(tc.run), Options{Webhook: true, Event: event})
		if err != nil {
			t.Fatalf("case %d: %v", index, err)
		}
		response := result.Response
		if response.Status != tc.status || response.Headers["Content-Type"] != tc.contentType || string(response.Body) != tc.body {
			t.Fatalf("case %d: unexpected response %d %#v %q", index, response.Status, response.Headers, response.Body)
		}
	}
	if _, err := runScript(t, bundle(`function () { return "nope"; }`), Options{Webhook: true, Event: event}); err == nil || !strings.Contains(err.Error(), "{status") {
		t.Fatalf("expected an invalid webhook return to fail, got %v", err)
	}
}

func TestLogLimits(t *testing.T) {
	result := mustRun(t, bundle(`function () {
		for (let i = 0; i < 1005; i++) console.log("line " + i);
	}`), Options{})
	if len(result.Logs) != MaxLogEntries+1 || !strings.Contains(result.Logs[MaxLogEntries].Message, "log limit") {
		t.Fatalf("expected the log to be capped, got %d entries", len(result.Logs))
	}
	result = mustRun(t, bundle(`function () { __autable_host.log("info", "x".repeat(10000), "y".repeat(70000)); }`), Options{})
	if len(result.Logs[0].Message) > maxLogMessage+4 || !strings.Contains(result.Logs[0].Data, "data omitted") {
		t.Fatalf("expected the message truncated and data replaced, got %d / %q", len(result.Logs[0].Message), result.Logs[0].Data)
	}
}

func TestStandardGlobals(t *testing.T) {
	result := mustRun(t, bundle(`function () {
		const url = new URL("/a b/c?x=1&y=two+words#frag", "https://user:pw@Example.com:443/base/");
		url.searchParams.append("z", "&=");
		const params = new URLSearchParams({ q: "a b", n: "1" });
		params.set("n", "2");
		const relative = new URL("../up?k=v", "http://host:8080/one/two/");
		let invalid = "";
		try { new URL("not a url"); } catch (error) { invalid = error.name; }
		const bytes = new TextEncoder().encode("名称€");
		return {
			href: url.href, host: url.host, origin: url.origin, pathname: url.pathname, y: url.searchParams.get("y"), hash: url.hash, username: url.username,
			params: params.toString(), entries: [...params], relative: relative.toString(), relativePort: relative.port,
			invalid, canParse: URL.canParse("https://ok.test"),
			bytes: Array.from(bytes), decoded: new TextDecoder().decode(bytes),
			btoa: btoa("hiÿ"), atob: atob("aGk="),
		};
	}`), Options{})
	var output map[string]any
	if err := json.Unmarshal([]byte(result.Output), &output); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"href":         "https://user:pw@example.com/a%20b/c?x=1&y=two+words&z=%26%3D#frag",
		"host":         "example.com",
		"origin":       "https://example.com",
		"pathname":     "/a%20b/c",
		"y":            "two words",
		"hash":         "#frag",
		"username":     "user",
		"params":       "q=a+b&n=2",
		"relative":     "http://host:8080/one/up?k=v",
		"relativePort": "8080",
		"invalid":      "TypeError",
		"canParse":     true,
		"decoded":      "名称€",
		"btoa":         "aGn/",
		"atob":         "hi",
	}
	for key, value := range want {
		if output[key] != value {
			t.Errorf("%s: got %#v, want %#v", key, output[key], value)
		}
	}
	if fmt.Sprint(output["bytes"]) != "[229 144 141 231 167 176 226 130 172]" {
		t.Errorf("unexpected utf-8 bytes %v", output["bytes"])
	}
}

func TestReadTriggersSandbox(t *testing.T) {
	program, err := Compile("workflow.js", `
		const fired = [];
		setTimeout(() => fired.push("timer"), 0);
		let hostError = "";
		try { __autable_host.secret("x"); } catch (error) { hostError = error.message; }
		var __workflow = {
			triggers: { start: { kind: "manual", fields: [{ name: "n", type: "number" }] }, note: { kind: "webhook", path: hostError === "" ? "open" : "sealed" } },
			run: function () {},
		};`)
	if err != nil {
		t.Fatal(err)
	}
	triggersJSON, err := ReadTriggers(context.Background(), program, Info{Database: "db", Workflow: "w"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(triggersJSON, `"path":"sealed"`) {
		t.Fatalf("expected host calls to throw while reading triggers, got %s", triggersJSON)
	}
	slow, _ := Compile("workflow.js", `for (;;) {}`)
	started := time.Now()
	if _, err := ReadTriggers(context.Background(), slow, Info{}); !errors.Is(err, ErrTimeout) || time.Since(started) > TriggerReadTimeout+2*time.Second {
		t.Fatalf("expected reading triggers to time out, got %v", err)
	}
	broken, _ := Compile("workflow.js", `var __workflow = { run: function () {} };`)
	if _, err := ReadTriggers(context.Background(), broken, Info{}); err == nil || !strings.Contains(err.Error(), `"triggers"`) {
		t.Fatalf("expected a missing triggers export to fail, got %v", err)
	}
}
