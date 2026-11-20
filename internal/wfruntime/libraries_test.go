package wfruntime

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeLibraries serves library sources by name, compiling them as the
// server does, and counts lookups.
type fakeLibraries struct {
	sources map[string]string
	lookups atomic.Int32
}

func (libraries *fakeLibraries) Library(_ context.Context, name string) (Library, error) {
	libraries.lookups.Add(1)
	source, ok := libraries.sources[name]
	if !ok {
		return Library{}, errors.New("it does not exist")
	}
	program, err := CompileLibrary(name, 3, source)
	if err != nil {
		return Library{}, err
	}
	return Library{Name: name, Version: 3, Program: program}, nil
}

// libBundle is what esbuild makes of a library: an IIFE assigning __lib.
func libBundle(body string) string {
	return "\"use strict\";\nvar __lib = (() => {\n" + body + "\n})();\n"
}

func TestWorkflowsImportLibrariesOncePerRun(t *testing.T) {
	libraries := &fakeLibraries{sources: map[string]string{
		// Counts its evaluations in a global, which a second evaluation
		// would bump.
		"greeting": libBundle(`
			globalThis.greetingLoads = (globalThis.greetingLoads || 0) + 1;
			const text = __autable_lib("@lib/text");
			return { greet: (name) => text.upper("hello " + name), token: () => __autable_host.secret("token") };`),
		"text": libBundle(`return { upper: (value) => value.toUpperCase() };`),
	}}
	source := `var __workflow = (() => {
		const greeting = __autable_lib("@lib/greeting");
		const again = __autable_lib("@lib/greeting");
		return {
			triggers: { start: { kind: "manual" } },
			run: async () => ({ text: greeting.greet("ada"), same: greeting === again, loads: globalThis.greetingLoads, token: greeting.token(), text2: __autable_lib("@lib/text").upper("x") }),
		};
	})();`
	result := mustRun(t, source, Options{Libraries: libraries, Secrets: map[string]string{"token": "s3"}})
	if result.Output != `{"text":"HELLO ADA","same":true,"loads":1,"token":"s3","text2":"X"}` {
		t.Fatalf("unexpected output %s", result.Output)
	}
	if want := []LibraryUse{{Name: "text", Version: 3}, {Name: "greeting", Version: 3}}; !slices.Equal(result.Libraries, want) {
		t.Fatalf("expected the libraries used in load order, got %#v", result.Libraries)
	}
	if libraries.lookups.Load() != 2 {
		t.Fatalf("expected one lookup per library per run, got %d", libraries.lookups.Load())
	}
	// A library leaves no global behind.
	leak := mustRun(t, `var __workflow = { triggers: {}, run: () => { __autable_lib("@lib/text"); return typeof __lib; } };`, Options{Libraries: libraries})
	if leak.Output != `"undefined"` {
		t.Fatalf("expected no __lib global, got %s", leak.Output)
	}
}

func TestLibraryImportFailures(t *testing.T) {
	libraries := &fakeLibraries{sources: map[string]string{
		"a":      libBundle(`return { b: __autable_lib("@lib/b") };`),
		"b":      libBundle(`return { a: __autable_lib("@lib/a") };`),
		"throws": libBundle(`throw new Error("broken at load");`),
		"empty":  `var other = 1;`,
	}}
	cases := map[string]string{
		`__autable_lib("@lib/a")`:       "library import cycle: a -> b -> a",
		`__autable_lib("@lib/missing")`: "library missing: it does not exist",
		`__autable_lib("@lib/throws")`:  "broken at load",
		`__autable_lib("@lib/empty")`:   "does not define __lib",
		`__autable_lib("lodash")`:       `libraries are imported as "@lib/<name>"`,
		`__autable_lib("@lib/Bad")`:     `libraries are imported as "@lib/<name>"`,
	}
	for call, want := range cases {
		_, err := runScript(t, bundle(fmt.Sprintf("function () { return %s; }", call)), Options{Libraries: libraries})
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: expected an error containing %q, got %v", call, want, err)
		}
	}
	if _, err := runScript(t, bundle(`function () { return __autable_lib("@lib/a"); }`), Options{}); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Errorf("expected libraries to be unavailable without a resolver, got %v", err)
	}
	// A failure inside a library is catchable by the importer.
	caught := mustRun(t, bundle(`function () { try { __autable_lib("@lib/throws"); } catch (error) { return "caught: " + error.message; } }`), Options{Libraries: libraries})
	if caught.Output != `"caught: broken at load"` {
		t.Fatalf("unexpected output %s", caught.Output)
	}
	// No global require: npm packages that detect Node keep their browser paths.
	if result := mustRun(t, bundle(`function () { return typeof require; }`), Options{Libraries: libraries}); result.Output != `"undefined"` {
		t.Fatalf("expected no global require, got %s", result.Output)
	}
}

func TestReadTriggersLoadsLibrariesInTheSandbox(t *testing.T) {
	libraries := &fakeLibraries{sources: map[string]string{
		"kinds": libBundle(`return { manual: () => ({ kind: "manual" }), secret: () => __autable_host.secret("x") };`),
	}}
	source := `var __workflow = (() => {
		const kinds = __autable_lib("@lib/kinds");
		return { triggers: { start: kinds.manual() }, run: () => kinds.secret() };
	})();`
	program, err := Compile("workflow.js", source)
	if err != nil {
		t.Fatal(err)
	}
	triggers, err := ReadTriggers(context.Background(), program, Info{}, libraries)
	if err != nil || triggers != `{"start":{"kind":"manual"}}` {
		t.Fatalf("unexpected triggers %s, %v", triggers, err)
	}
	if _, err := ReadTriggers(context.Background(), program, Info{}, nil); err == nil || !strings.Contains(err.Error(), "not available") {
		t.Fatalf("expected the import to fail without a resolver, got %v", err)
	}
}

func TestImportsFindsLibraryCalls(t *testing.T) {
	source := `var a = __toESM(__autable_lib("@lib/text")); var b = __autable_lib( '@lib/format' ); __autable_lib("@lib/text"); require("@lib/ignored"); x__autable_lib("@lib/nope");`
	if got := Imports(source); !slices.Equal(got, []string{"format", "text"}) {
		t.Fatalf("unexpected imports %v", got)
	}
	if got := Imports("var x = 1;"); len(got) != 0 {
		t.Fatalf("expected no imports, got %v", got)
	}
}
