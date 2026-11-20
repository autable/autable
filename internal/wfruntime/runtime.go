// Package wfruntime runs workflow bundles: one goja runtime per run, a
// small event loop that settles host promises, the standard globals, and
// the host API of docs/design/workflows-host-api.md. It knows nothing
// about storage or permissions; the caller supplies a Host.
package wfruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dop251/goja"

	"autable/internal/egress"
)

// HostVersion is the version of the host API, equal to the workflow SDK
// version that introduced it.
const HostVersion = "1.0"

const (
	// MaxOutputBytes caps a run's JSON output.
	MaxOutputBytes = 1 << 20
	// MaxLogEntries caps the log entries of one run.
	MaxLogEntries = 1000
	maxLogMessage = 8 << 10
	maxLogData    = 64 << 10
	// TriggerReadTimeout bounds evaluating a bundle to read its triggers.
	TriggerReadTimeout = 5 * time.Second
	maxCallStackSize   = 4096
)

// Host performs the operations that leave the runtime. Every method is
// called on its own goroutine with the run's context; results must be
// JSON-like Go values ([]byte becomes an ArrayBuffer).
type Host interface {
	Fetch(ctx context.Context, via string, request egress.Request) (egress.Response, error)
	Table(ctx context.Context, op, table string, args map[string]any) (any, error)
	Users(ctx context.Context, op string, args map[string]any) (any, error)
	Files(ctx context.Context, op string, args map[string]any) (any, error)
}

// Info describes the run to the code (__autable_host.info).
type Info struct {
	Database string
	Workflow string
	Version  int64
	RunID    int64
	Trigger  string
}

// Options configure one run.
type Options struct {
	Program *goja.Program
	Info    Info
	// Event is passed to run(); []byte values become ArrayBuffers.
	Event   map[string]any
	Secrets map[string]string
	Vars    map[string]string
	Host    Host
	// Timeout bounds the whole run, enforced with Interrupt.
	Timeout time.Duration
	// Webhook makes the result an HTTP response instead of JSON output.
	Webhook bool
	// Libraries resolves require("@lib/<name>"); nil makes every library
	// import fail.
	Libraries LibraryResolver
}

// Log is one entry of the run log. Data is JSON, empty when absent.
type Log struct {
	Time    int64
	Level   string
	Message string
	Data    string
}

// WebhookResponse is what a webhook run answers.
type WebhookResponse struct {
	Status  int
	Headers map[string]string
	Body    []byte
}

// Result is the outcome of a run. Logs are kept even when the run fails.
type Result struct {
	// Output is the JSON of the resolved value, empty for undefined.
	Output   string
	Response *WebhookResponse
	Logs     []Log
	// Libraries are the libraries the run loaded, in load order.
	Libraries []LibraryUse
}

// Error kinds the caller maps to statuses.
var (
	ErrTimeout   = errors.New("run exceeded its time limit")
	ErrCancelled = errors.New("run was cancelled")
)

// Compile parses a bundle script once; the program can be run any number
// of times, concurrently, in separate runtimes.
func Compile(name, source string) (*goja.Program, error) {
	program, err := goja.Compile(name, source, false)
	if err != nil {
		return nil, fmt.Errorf("compile %s: %w", name, err)
	}
	return program, nil
}

// Run executes the bundle and calls __workflow.run(event).
func Run(ctx context.Context, options Options) (Result, error) {
	if options.Timeout <= 0 {
		return Result{}, errors.New("a run needs a timeout")
	}
	ctx, cancel := context.WithTimeout(ctx, options.Timeout)
	defer cancel()
	vm := newVM(ctx, options, false)
	defer vm.stop()

	value, err := vm.execute(func() (goja.Value, error) {
		run, err := vm.workflowExport("run")
		if err != nil {
			return nil, err
		}
		fn, ok := goja.AssertFunction(run)
		if !ok {
			return nil, errors.New("the bundle's run export is not a function")
		}
		return fn(goja.Undefined(), vm.toJS(options.Event))
	})
	result := Result{Logs: vm.logs, Libraries: vm.libUses}
	if err != nil {
		return result, err
	}
	if options.Webhook {
		response, err := vm.webhookResponse(value)
		result.Response = response
		result.Logs = vm.logs
		return result, err
	}
	output, err := vm.outputJSON(value)
	result.Output = output
	return result, err
}

// ReadTriggers evaluates the bundle with every host function disabled and
// returns the JSON of its `triggers` export. It also checks that `run` is
// a function. Libraries the bundle imports are loaded through libraries
// (evaluated with host functions disabled too). Validate the result with
// ParseTriggers.
func ReadTriggers(ctx context.Context, program *goja.Program, info Info, libraries LibraryResolver) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, TriggerReadTimeout)
	defer cancel()
	vm := newVM(ctx, Options{Program: program, Info: info, Libraries: libraries}, true)
	defer vm.stop()
	value, err := vm.execute(func() (goja.Value, error) {
		run, err := vm.workflowExport("run")
		if err != nil {
			return nil, err
		}
		if _, ok := goja.AssertFunction(run); !ok {
			return nil, errors.New("the bundle's run export is not a function")
		}
		triggers, err := vm.workflowExport("triggers")
		if err != nil {
			return nil, err
		}
		object, ok := triggers.(*goja.Object)
		if !ok || goja.IsNull(triggers) {
			return nil, errors.New("the bundle's triggers export must be an object")
		}
		return vm.stringify(object)
	})
	if err != nil {
		return "", err
	}
	if goja.IsUndefined(value) {
		return "", errors.New("the bundle's triggers export cannot be encoded as JSON")
	}
	return value.String(), nil
}

// vm is one runtime with its event loop.
type vm struct {
	ctx      context.Context
	rt       *goja.Runtime
	options  Options
	sandbox  bool
	queue    chan func()
	done     chan struct{}
	pending  int
	timers   map[int64]*time.Timer
	nextID   int64
	fatal    error
	logs     []Log
	dropped  bool
	isView   goja.Callable
	jsonStr  goja.Callable
	errorCtr goja.Value
	// Libraries loaded in this run, the ones being loaded (for cycles),
	// and what was used.
	libExports map[string]goja.Value
	libLoading []string
	libUses    []LibraryUse
}

func newVM(ctx context.Context, options Options, sandbox bool) *vm {
	rt := goja.New()
	rt.SetMaxCallStackSize(maxCallStackSize)
	machine := &vm{
		ctx:     ctx,
		rt:      rt,
		options: options,
		sandbox: sandbox,
		queue:   make(chan func(), 64),
		done:    make(chan struct{}),
		timers:  map[int64]*time.Timer{},

		libExports: map[string]goja.Value{},
	}
	machine.isView, _ = goja.AssertFunction(rt.Get("ArrayBuffer").ToObject(rt).Get("isView"))
	machine.jsonStr, _ = goja.AssertFunction(rt.Get("JSON").ToObject(rt).Get("stringify"))
	machine.errorCtr = rt.Get("Error")
	return machine
}

func (machine *vm) stop() {
	close(machine.done)
	for _, timer := range machine.timers {
		timer.Stop()
	}
}

// execute installs the globals, runs the bundle, calls start, and drives
// the event loop until the returned value (a promise or not) settles.
func (machine *vm) execute(start func() (goja.Value, error)) (value goja.Value, err error) {
	watchDone := make(chan struct{})
	defer close(watchDone)
	go func() {
		select {
		case <-machine.ctx.Done():
			machine.rt.Interrupt(machine.ctx.Err())
		case <-watchDone:
		}
	}()
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("workflow runtime panic: %v", recovered)
		}
		if err != nil {
			err = machine.translate(err)
		}
	}()
	if err := machine.installGlobals(); err != nil {
		return nil, err
	}
	if _, err := machine.rt.RunProgram(machine.options.Program); err != nil {
		return nil, err
	}
	value, err = start()
	if err != nil {
		return nil, err
	}
	if err := machine.fatal; err != nil {
		return nil, err
	}
	promise, ok := value.Export().(*goja.Promise)
	if !ok {
		return value, nil
	}
	for promise.State() == goja.PromiseStatePending {
		if machine.pending == 0 {
			return nil, errors.New("run never settles: its promise is pending with nothing left to wait for")
		}
		select {
		case job := <-machine.queue:
			job()
			if machine.fatal != nil {
				return nil, machine.fatal
			}
		case <-machine.ctx.Done():
			return nil, machine.ctx.Err()
		}
	}
	if promise.State() == goja.PromiseStateRejected {
		return nil, machine.rejection(promise.Result())
	}
	return promise.Result(), nil
}

// translate turns goja and context errors into run errors.
func (machine *vm) translate(err error) error {
	var interrupted *goja.InterruptedError
	if errors.As(err, &interrupted) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		switch {
		case errors.Is(machine.ctx.Err(), context.DeadlineExceeded):
			return fmt.Errorf("%w (%s)", ErrTimeout, machine.options.Timeout)
		case machine.ctx.Err() != nil:
			return ErrCancelled
		}
	}
	var exception *goja.Exception
	if errors.As(err, &exception) {
		return errors.New(exceptionText(exception))
	}
	return err
}

func exceptionText(exception *goja.Exception) string {
	text := strings.TrimSpace(exception.Error())
	return text
}

// rejection describes a rejected run promise like an uncaught exception.
func (machine *vm) rejection(reason goja.Value) error {
	if object, ok := reason.(*goja.Object); ok {
		if stack := object.Get("stack"); stack != nil && !goja.IsUndefined(stack) && stack.String() != "" {
			return errors.New(strings.TrimSpace(stack.String()))
		}
	}
	if reason == nil {
		return errors.New("run rejected")
	}
	return errors.New(reason.String())
}

func (machine *vm) workflowExport(name string) (goja.Value, error) {
	exports := machine.rt.GlobalObject().Get("__workflow")
	if exports == nil || goja.IsUndefined(exports) || goja.IsNull(exports) {
		return nil, errors.New("the bundle does not define globalThis.__workflow (build it as an IIFE with --global-name=__workflow)")
	}
	value := exports.ToObject(machine.rt).Get(name)
	if value == nil || goja.IsUndefined(value) {
		return nil, fmt.Errorf("the bundle does not export %q", name)
	}
	return value, nil
}

func (machine *vm) stringify(value goja.Value) (goja.Value, error) {
	return machine.jsonStr(goja.Undefined(), value)
}

func (machine *vm) outputJSON(value goja.Value) (string, error) {
	if value == nil || goja.IsUndefined(value) {
		return "", nil
	}
	encoded, err := machine.stringify(value)
	if err != nil {
		return "", machine.translate(err)
	}
	if goja.IsUndefined(encoded) {
		return "", nil
	}
	output := encoded.String()
	if len(output) > MaxOutputBytes {
		return "", fmt.Errorf("run output is %d bytes; the limit is %d", len(output), MaxOutputBytes)
	}
	return output, nil
}

func (machine *vm) webhookResponse(value goja.Value) (*WebhookResponse, error) {
	if value == nil || goja.IsUndefined(value) {
		return &WebhookResponse{Status: 204}, nil
	}
	object, ok := value.(*goja.Object)
	if !ok || goja.IsNull(value) {
		return nil, errors.New("a webhook run must return {status, headers?, body?} or nothing")
	}
	statusValue := object.Get("status")
	if statusValue == nil || goja.IsUndefined(statusValue) {
		return nil, errors.New("a webhook run must return {status, headers?, body?} or nothing")
	}
	status := statusValue.ToInteger()
	if status < 100 || status > 599 {
		return nil, fmt.Errorf("webhook response status %d is not an HTTP status", status)
	}
	response := &WebhookResponse{Status: int(status), Headers: map[string]string{}}
	if headers := object.Get("headers"); headers != nil && !goja.IsUndefined(headers) && !goja.IsNull(headers) {
		headerObject := headers.ToObject(machine.rt)
		for _, key := range headerObject.Keys() {
			response.Headers[key] = headerObject.Get(key).String()
		}
	}
	contentType := ""
	body := object.Get("body")
	switch {
	case body == nil || goja.IsUndefined(body) || goja.IsNull(body):
	case isString(body):
		response.Body = []byte(body.String())
		contentType = "text/plain; charset=utf-8"
	default:
		if data, ok := machine.bytesOf(body); ok {
			response.Body = data
			contentType = "application/octet-stream"
			break
		}
		encoded, err := machine.outputJSON(body)
		if err != nil {
			return nil, err
		}
		response.Body = []byte(encoded)
		contentType = "application/json"
	}
	if contentType != "" && !hasHeader(response.Headers, "content-type") {
		response.Headers["Content-Type"] = contentType
	}
	return response, nil
}

func hasHeader(headers map[string]string, name string) bool {
	for key := range headers {
		if strings.EqualFold(key, name) {
			return true
		}
	}
	return false
}

func isString(value goja.Value) bool {
	_, ok := value.Export().(string)
	return ok
}

// bytesOf returns a copy of the bytes of an ArrayBuffer or view.
func (machine *vm) bytesOf(value goja.Value) ([]byte, bool) {
	if value == nil {
		return nil, false
	}
	if buffer, ok := value.Export().(goja.ArrayBuffer); ok {
		return append([]byte(nil), buffer.Bytes()...), true
	}
	if _, ok := value.(*goja.Object); !ok {
		return nil, false
	}
	view, err := machine.isView(goja.Undefined(), value)
	if err != nil || !view.ToBoolean() {
		return nil, false
	}
	var data []byte
	if err := machine.rt.ExportTo(value, &data); err != nil {
		return nil, false
	}
	return append([]byte(nil), data...), true
}

// toJS converts a JSON-like Go value into plain JavaScript values, so
// host results behave like ordinary objects and arrays.
func (machine *vm) toJS(value any) goja.Value {
	rt := machine.rt
	switch typed := value.(type) {
	case nil:
		return goja.Null()
	case goja.Value:
		return typed
	case bool, string, int, int32, int64, uint32, float32, float64:
		return rt.ToValue(typed)
	case json.Number:
		if integer, err := typed.Int64(); err == nil {
			return rt.ToValue(integer)
		}
		float, _ := typed.Float64()
		return rt.ToValue(float)
	case []byte:
		return rt.ToValue(rt.NewArrayBuffer(append([]byte(nil), typed...)))
	case map[string]any:
		// Sorted keys: property order must not depend on Go map order.
		object := rt.NewObject()
		for _, key := range sortedKeys(typed) {
			_ = object.Set(key, machine.toJS(typed[key]))
		}
		return object
	case map[string]string:
		object := rt.NewObject()
		for _, key := range sortedKeys(typed) {
			_ = object.Set(key, typed[key])
		}
		return object
	case []any:
		items := make([]any, len(typed))
		for index, item := range typed {
			items[index] = machine.toJS(item)
		}
		return rt.NewArray(items...)
	case []string:
		items := make([]any, len(typed))
		for index, item := range typed {
			items[index] = item
		}
		return rt.NewArray(items...)
	default:
		// Structs and other typed values: through their JSON form.
		encoded, err := json.Marshal(typed)
		if err != nil {
			panic(rt.NewGoError(err))
		}
		decoder := json.NewDecoder(strings.NewReader(string(encoded)))
		decoder.UseNumber()
		var generic any
		if err := decoder.Decode(&generic); err != nil {
			panic(rt.NewGoError(err))
		}
		return machine.toJS(generic)
	}
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// post runs job on the loop; it is dropped when the run already ended.
func (machine *vm) post(job func()) {
	select {
	case machine.queue <- job:
	case <-machine.done:
	}
}

// async starts work on a goroutine and returns a promise the loop settles
// with its result.
func (machine *vm) async(work func(ctx context.Context) (any, error)) goja.Value {
	promise, resolve, reject := machine.rt.NewPromise()
	machine.pending++
	go func() {
		value, err := work(machine.ctx)
		machine.post(func() {
			machine.pending--
			var settleErr error
			if err != nil {
				settleErr = reject(machine.newError(err))
			} else {
				settleErr = resolve(machine.toJS(value))
			}
			if settleErr != nil && machine.fatal == nil {
				machine.fatal = settleErr
			}
		})
	}()
	return machine.rt.ToValue(promise)
}

// HostError lets a Host give an error a JavaScript name such as
// "PermissionError" or "NotFoundError".
type HostError struct {
	Name string
	Err  error
}

func (err *HostError) Error() string { return err.Err.Error() }
func (err *HostError) Unwrap() error { return err.Err }

func (machine *vm) newError(err error) *goja.Object {
	object, constructErr := machine.rt.New(machine.errorCtr, machine.rt.ToValue(err.Error()))
	if constructErr != nil {
		return machine.rt.NewGoError(err)
	}
	var hostErr *HostError
	if errors.As(err, &hostErr) && hostErr.Name != "" {
		_ = object.Set("name", hostErr.Name)
	}
	return object
}

// throw raises err as a JavaScript Error from a sync host function.
func (machine *vm) throw(err error) {
	panic(machine.newError(err))
}

func (machine *vm) addLog(level, message string, data goja.Value) {
	if len(machine.logs) >= MaxLogEntries {
		if !machine.dropped {
			machine.dropped = true
			machine.logs = append(machine.logs, Log{Time: time.Now().UnixMilli(), Level: "warn", Message: fmt.Sprintf("log limit of %d entries reached; later entries are dropped", MaxLogEntries)})
		}
		return
	}
	switch level {
	case "info", "warn", "error":
	default:
		level = "info"
	}
	if len(message) > maxLogMessage {
		message = message[:maxLogMessage] + "…"
	}
	entry := Log{Time: time.Now().UnixMilli(), Level: level, Message: message}
	if data != nil && !goja.IsUndefined(data) {
		encoded, err := machine.stringify(data)
		switch {
		case err != nil:
			entry.Data = mustJSON("data could not be encoded: " + err.Error())
		case goja.IsUndefined(encoded):
		case len(encoded.String()) > maxLogData:
			entry.Data = mustJSON(fmt.Sprintf("data omitted: %d bytes exceed %d KiB", len(encoded.String()), maxLogData>>10))
		default:
			entry.Data = encoded.String()
		}
	}
	machine.logs = append(machine.logs, entry)
}

func mustJSON(value any) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
