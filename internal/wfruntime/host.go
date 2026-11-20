package wfruntime

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"
	"time"

	"github.com/dop251/goja"

	"autable/internal/egress"
)

var errSandbox = errors.New("host calls are not available while reading triggers")

// installGlobals defines __autable_host, the timers, console and the
// standard globals implemented by the prelude.
func (machine *vm) installGlobals() error {
	rt := machine.rt
	global := rt.GlobalObject()

	native := rt.NewObject()
	set := func(object *goja.Object, name string, fn func(goja.FunctionCall) goja.Value) {
		_ = object.Set(name, fn)
	}
	set(native, "utf8Encode", func(call goja.FunctionCall) goja.Value {
		return rt.ToValue(rt.NewArrayBuffer([]byte(call.Argument(0).String())))
	})
	set(native, "utf8Decode", func(call goja.FunctionCall) goja.Value {
		data := machine.bytesArg(call.Argument(0), "data", false)
		return rt.ToValue(strings.ToValidUTF8(string(data), "�"))
	})
	set(native, "btoa", func(call goja.FunctionCall) goja.Value {
		text := call.Argument(0).String()
		data := make([]byte, 0, len(text))
		for _, char := range text {
			if char > 0xFF {
				machine.throwNamed("InvalidCharacterError", errors.New("btoa: the string contains characters outside of the Latin-1 range"))
			}
			data = append(data, byte(char))
		}
		return rt.ToValue(base64.StdEncoding.EncodeToString(data))
	})
	set(native, "atob", func(call goja.FunctionCall) goja.Value {
		text := strings.Map(func(char rune) rune {
			if char == ' ' || char == '\t' || char == '\n' || char == '\f' || char == '\r' {
				return -1
			}
			return char
		}, call.Argument(0).String())
		data, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(text, "="))
		if err != nil {
			machine.throwNamed("InvalidCharacterError", errors.New("atob: the string is not correctly encoded"))
		}
		var builder strings.Builder
		for _, value := range data {
			builder.WriteRune(rune(value))
		}
		return rt.ToValue(builder.String())
	})
	set(native, "parseURL", func(call goja.FunctionCall) goja.Value {
		return machine.parseURL(call.Argument(0), call.Argument(1))
	})
	set(native, "log", func(call goja.FunctionCall) goja.Value {
		machine.addLog(call.Argument(0).String(), call.Argument(1).String(), call.Argument(2))
		return goja.Undefined()
	})
	set(native, "setTimeout", func(call goja.FunctionCall) goja.Value {
		return machine.setTimeout(call)
	})
	set(native, "clearTimeout", func(call goja.FunctionCall) goja.Value {
		machine.clearTimeout(call.Argument(0))
		return goja.Undefined()
	})
	if err := global.Set("__autable_native", native); err != nil {
		return err
	}
	if _, err := rt.RunString(prelude); err != nil {
		return err
	}
	if err := global.Delete("__autable_native"); err != nil {
		return err
	}
	return global.Set("__autable_host", machine.hostObject())
}

func (machine *vm) setTimeout(call goja.FunctionCall) goja.Value {
	callback, ok := goja.AssertFunction(call.Argument(0))
	if !ok {
		panic(machine.rt.NewTypeError("setTimeout needs a function"))
	}
	delay := call.Argument(1).ToFloat()
	if math.IsNaN(delay) || delay < 0 {
		delay = 0
	}
	args := append([]goja.Value(nil), call.Arguments[min(2, len(call.Arguments)):]...)
	machine.nextID++
	id := machine.nextID
	if machine.sandbox {
		// Timers never fire while triggers are read.
		return machine.rt.ToValue(id)
	}
	machine.pending++
	machine.timers[id] = time.AfterFunc(time.Duration(delay*float64(time.Millisecond)), func() {
		machine.post(func() {
			if _, ok := machine.timers[id]; !ok {
				return
			}
			delete(machine.timers, id)
			machine.pending--
			if _, err := callback(goja.Undefined(), args...); err != nil && machine.fatal == nil {
				machine.fatal = err
			}
		})
	})
	return machine.rt.ToValue(id)
}

func (machine *vm) clearTimeout(idValue goja.Value) {
	id := idValue.ToInteger()
	timer, ok := machine.timers[id]
	if !ok {
		return
	}
	timer.Stop()
	delete(machine.timers, id)
	machine.pending--
}

func (machine *vm) parseURL(input, base goja.Value) goja.Value {
	rt := machine.rt
	raw := strings.TrimSpace(input.String())
	parsed, err := url.Parse(raw)
	if err == nil && base != nil && !goja.IsUndefined(base) && !goja.IsNull(base) {
		var baseURL *url.URL
		baseURL, err = url.Parse(strings.TrimSpace(base.String()))
		if err == nil && (baseURL.Scheme == "" || !baseURL.IsAbs()) {
			err = fmt.Errorf("invalid base URL %q", base.String())
		}
		if err == nil {
			parsed = baseURL.ResolveReference(parsed)
		}
	}
	if err == nil && (parsed.Scheme == "" || (parsed.Host == "" && parsed.Opaque == "" && parsed.Path == "")) {
		err = fmt.Errorf("invalid URL %q", raw)
	}
	if err != nil {
		panic(rt.NewTypeError(fmt.Sprintf("Invalid URL: %s", raw)))
	}
	parts := rt.NewObject()
	_ = parts.Set("protocol", strings.ToLower(parsed.Scheme)+":")
	username, password := "", ""
	if parsed.User != nil {
		username = parsed.User.Username()
		password, _ = parsed.User.Password()
	}
	_ = parts.Set("username", username)
	_ = parts.Set("password", password)
	_ = parts.Set("hostname", strings.ToLower(parsed.Hostname()))
	port := parsed.Port()
	if (parsed.Scheme == "http" && port == "80") || (parsed.Scheme == "https" && port == "443") {
		port = ""
	}
	_ = parts.Set("port", port)
	pathname := parsed.EscapedPath()
	if parsed.Opaque != "" {
		pathname = parsed.Opaque
	} else if pathname == "" && parsed.Host != "" {
		pathname = "/"
	}
	_ = parts.Set("pathname", pathname)
	search := ""
	if parsed.RawQuery != "" {
		search = "?" + parsed.RawQuery
	}
	_ = parts.Set("search", search)
	hash := ""
	if parsed.Fragment != "" {
		hash = "#" + parsed.EscapedFragment()
	}
	_ = parts.Set("hash", hash)
	_ = parts.Set("special", parsed.Host != "" || parsed.Opaque == "")
	return parts
}

// hostObject builds __autable_host.
func (machine *vm) hostObject() *goja.Object {
	rt := machine.rt
	host := rt.NewObject()
	info := rt.NewObject()
	_ = info.Set("database", machine.options.Info.Database)
	_ = info.Set("workflow", machine.options.Info.Workflow)
	_ = info.Set("version", machine.options.Info.Version)
	_ = info.Set("runId", machine.options.Info.RunID)
	_ = info.Set("trigger", machine.options.Info.Trigger)
	machine.freeze(info)
	_ = host.Set("version", HostVersion)
	_ = host.Set("info", info)

	// sync wraps a host function that does no I/O; in the trigger sandbox
	// every host function throws.
	sync := func(name string, fn func(goja.FunctionCall) goja.Value) {
		_ = host.Set(name, func(call goja.FunctionCall) goja.Value {
			if machine.sandbox {
				machine.throw(errSandbox)
			}
			return fn(call)
		})
	}
	sync("fetch", machine.hostFetch)
	sync("hash", machine.hostHash)
	sync("hmac", machine.hostHMAC)
	sync("randomBytes", machine.hostRandomBytes)
	sync("sign", machine.hostSign)
	sync("verify", machine.hostVerify)
	sync("encrypt", machine.hostEncrypt)
	sync("decrypt", machine.hostDecrypt)
	sync("timingSafeEqual", machine.hostTimingSafeEqual)
	sync("encode", machine.hostEncode)
	sync("decode", machine.hostDecode)
	sync("secret", func(call goja.FunctionCall) goja.Value {
		value, ok := machine.options.Secrets[call.Argument(0).String()]
		if !ok {
			return goja.Undefined()
		}
		return rt.ToValue(value)
	})
	sync("variable", func(call goja.FunctionCall) goja.Value {
		value, ok := machine.options.Vars[call.Argument(0).String()]
		if !ok {
			return goja.Undefined()
		}
		return rt.ToValue(value)
	})
	sync("log", func(call goja.FunctionCall) goja.Value {
		machine.addLog(call.Argument(0).String(), call.Argument(1).String(), call.Argument(2))
		return goja.Undefined()
	})
	sync("table", func(call goja.FunctionCall) goja.Value {
		op, table := call.Argument(0).String(), call.Argument(1).String()
		args := machine.argsObject(call.Argument(2))
		return machine.async(func(ctx context.Context) (any, error) {
			return machine.options.Host.Table(ctx, op, table, args)
		})
	})
	sync("users", func(call goja.FunctionCall) goja.Value {
		op := call.Argument(0).String()
		args := machine.argsObject(call.Argument(1))
		return machine.async(func(ctx context.Context) (any, error) {
			return machine.options.Host.Users(ctx, op, args)
		})
	})
	sync("files", func(call goja.FunctionCall) goja.Value {
		op := call.Argument(0).String()
		args := machine.argsObject(call.Argument(1))
		// Upload bodies are bytes; Export would turn them into opaque values.
		if body := call.Argument(1); op == "upload" && !goja.IsUndefined(body) && !goja.IsNull(body) {
			if data := body.ToObject(rt).Get("body"); data != nil && !goja.IsUndefined(data) {
				args["body"] = machine.bytesArg(data, "body", true)
			}
		}
		return machine.async(func(ctx context.Context) (any, error) {
			return machine.options.Host.Files(ctx, op, args)
		})
	})
	machine.freeze(host)
	return host
}

func (machine *vm) freeze(object *goja.Object) {
	freeze, _ := goja.AssertFunction(machine.rt.Get("Object").ToObject(machine.rt).Get("freeze"))
	_, _ = freeze(goja.Undefined(), object)
}

// argsObject exports a plain argument object to Go.
func (machine *vm) argsObject(value goja.Value) map[string]any {
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return map[string]any{}
	}
	exported, ok := value.Export().(map[string]any)
	if !ok {
		panic(machine.rt.NewTypeError("host call arguments must be an object"))
	}
	return exported
}

func (machine *vm) hostFetch(call goja.FunctionCall) goja.Value {
	rt := machine.rt
	argument := call.Argument(0)
	if goja.IsUndefined(argument) || goja.IsNull(argument) {
		panic(rt.NewTypeError("fetch needs a request object"))
	}
	object := argument.ToObject(rt)
	request := egress.Request{URL: stringField(object, "url"), Method: stringField(object, "method")}
	via := stringField(object, "via")
	if timeout := object.Get("timeoutMs"); timeout != nil && !goja.IsUndefined(timeout) && !goja.IsNull(timeout) {
		request.TimeoutMs = timeout.ToInteger()
		if request.TimeoutMs <= 0 {
			panic(rt.NewTypeError("fetch timeoutMs must be positive"))
		}
	}
	if deadline, ok := machine.ctx.Deadline(); ok {
		left := time.Until(deadline).Milliseconds()
		if request.Timeout().Milliseconds() > left {
			request.TimeoutMs = max(left, 1)
		}
	}
	if headers := object.Get("headers"); headers != nil && !goja.IsUndefined(headers) && !goja.IsNull(headers) {
		request.Headers = machine.headerPairs(headers)
	}
	if body := object.Get("body"); body != nil && !goja.IsUndefined(body) && !goja.IsNull(body) {
		request.Body = machine.bytesArg(body, "body", true)
	}
	if err := request.Validate(); err != nil {
		machine.throw(err)
	}
	return machine.async(func(ctx context.Context) (any, error) {
		response, err := machine.options.Host.Fetch(ctx, via, request)
		if err != nil {
			return nil, err
		}
		headers := make([]any, 0, len(response.Headers))
		for _, header := range response.Headers {
			headers = append(headers, []any{header[0], header[1]})
		}
		return map[string]any{
			"status":     response.Status,
			"statusText": response.StatusText,
			"url":        response.URL,
			"headers":    headers,
			"body":       response.Body,
		}, nil
	})
}

func (machine *vm) headerPairs(value goja.Value) [][2]string {
	rt := machine.rt
	object := value.ToObject(rt)
	var pairs [][2]string
	if isArray(rt, value) {
		length := object.Get("length").ToInteger()
		for index := int64(0); index < length; index++ {
			pair := object.Get(fmt.Sprint(index)).ToObject(rt)
			pairs = append(pairs, [2]string{pair.Get("0").String(), pair.Get("1").String()})
		}
		return pairs
	}
	for _, key := range object.Keys() {
		pairs = append(pairs, [2]string{key, object.Get(key).String()})
	}
	return pairs
}

func isArray(rt *goja.Runtime, value goja.Value) bool {
	isArrayFn, _ := goja.AssertFunction(rt.Get("Array").ToObject(rt).Get("isArray"))
	result, err := isArrayFn(goja.Undefined(), value)
	return err == nil && result.ToBoolean()
}

func stringField(object *goja.Object, name string) string {
	value := object.Get(name)
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		return ""
	}
	return value.String()
}

// bytesArg reads a bytes argument; strings are accepted as UTF-8 when
// allowString is set.
func (machine *vm) bytesArg(value goja.Value, name string, allowString bool) []byte {
	if value != nil && isString(value) {
		if !allowString {
			panic(machine.rt.NewTypeError(fmt.Sprintf("%s must be an ArrayBuffer or a typed array", name)))
		}
		return []byte(value.String())
	}
	data, ok := machine.bytesOf(value)
	if !ok {
		if allowString {
			panic(machine.rt.NewTypeError(fmt.Sprintf("%s must be a string, an ArrayBuffer or a typed array", name)))
		}
		panic(machine.rt.NewTypeError(fmt.Sprintf("%s must be an ArrayBuffer or a typed array", name)))
	}
	return data
}

func (machine *vm) throwNamed(name string, err error) {
	machine.throw(&HostError{Name: name, Err: err})
}

func (machine *vm) buffer(data []byte) goja.Value {
	return machine.rt.ToValue(machine.rt.NewArrayBuffer(data))
}
