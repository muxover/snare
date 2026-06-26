package proxy

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/dop251/goja"
	"github.com/muxover/snare/v2/capture"
)

// HookEngine runs JS hook files per request/response/capture.
// All methods are no-ops on a nil receiver.
type HookEngine struct {
	files []string
	log   *slog.Logger
}

func NewHookEngine(files []string, log *slog.Logger) *HookEngine {
	if len(files) == 0 {
		return nil
	}
	return &HookEngine{files: files, log: log}
}

// HookShortCircuit is returned when onRequest returns a response object,
// meaning the origin should not be contacted.
type HookShortCircuit struct {
	Status  int
	Headers map[string]string
	Body    string
}

func newRT(log *slog.Logger) *goja.Runtime {
	rt := goja.New()
	console := rt.NewObject()
	_ = console.Set("log", func(call goja.FunctionCall) goja.Value {
		parts := make([]string, len(call.Arguments))
		for i, a := range call.Arguments {
			parts[i] = a.String()
		}
		log.Debug("[hook]", "msg", strings.Join(parts, " "))
		return goja.Undefined()
	})
	_ = rt.Set("console", console)
	return rt
}

func getCallable(rt *goja.Runtime, name string) (goja.Callable, bool) {
	v := rt.Get(name)
	if v == nil || goja.IsUndefined(v) || goja.IsNull(v) {
		return nil, false
	}
	fn, ok := goja.AssertFunction(v)
	return fn, ok
}

func headersToObj(rt *goja.Runtime, h http.Header) *goja.Object {
	obj := rt.NewObject()
	for k, vs := range h {
		if len(vs) > 0 {
			_ = obj.Set(k, vs[0])
		}
	}
	return obj
}

func objToHeaders(obj *goja.Object, h http.Header) {
	for k := range h {
		delete(h, k)
	}
	for _, k := range obj.Keys() {
		v := obj.Get(k)
		if v != nil && !goja.IsUndefined(v) {
			h.Set(k, v.String())
		}
	}
}

func (h *HookEngine) RunOnRequest(req *http.Request, body []byte) (*HookShortCircuit, []byte) {
	if h == nil {
		return nil, body
	}
	for _, path := range h.files {
		sc, newBody, err := h.runOnRequestFile(path, req, body)
		if err != nil {
			h.log.Warn("hook onRequest error", "file", path, "err", err)
			continue
		}
		body = newBody
		if sc != nil {
			return sc, body
		}
	}
	return nil, body
}

func (h *HookEngine) runOnRequestFile(path string, req *http.Request, body []byte) (*HookShortCircuit, []byte, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, body, err
	}
	rt := newRT(h.log)
	if _, err := rt.RunString(string(src)); err != nil {
		return nil, body, err
	}
	fn, ok := getCallable(rt, "onRequest")
	if !ok {
		return nil, body, nil
	}

	reqObj := rt.NewObject()
	_ = reqObj.Set("method", req.Method)
	_ = reqObj.Set("url", req.URL.String())
	_ = reqObj.Set("headers", headersToObj(rt, req.Header))
	_ = reqObj.Set("body", string(body))

	result, err := fn(goja.Undefined(), reqObj)
	if err != nil {
		return nil, body, err
	}

	if m := reqObj.Get("method"); m != nil && !goja.IsUndefined(m) {
		req.Method = m.String()
	}
	if u := reqObj.Get("url"); u != nil && !goja.IsUndefined(u) {
		if parsed, pErr := url.Parse(u.String()); pErr == nil {
			req.URL = parsed
		}
	}
	if hv := reqObj.Get("headers"); hv != nil && !goja.IsUndefined(hv) && !goja.IsNull(hv) {
		if hObj, ok2 := hv.(*goja.Object); ok2 {
			objToHeaders(hObj, req.Header)
		}
	}
	if b := reqObj.Get("body"); b != nil && !goja.IsUndefined(b) {
		body = []byte(b.String())
	}

	if result == nil || goja.IsUndefined(result) || goja.IsNull(result) {
		return nil, body, nil
	}
	resObj, ok2 := result.(*goja.Object)
	if !ok2 {
		return nil, body, nil
	}
	sc := &HookShortCircuit{Headers: map[string]string{}}
	if s := resObj.Get("status"); s != nil && !goja.IsUndefined(s) {
		sc.Status = int(s.ToInteger())
	}
	if sc.Status == 0 {
		sc.Status = http.StatusOK
	}
	if b := resObj.Get("body"); b != nil && !goja.IsUndefined(b) {
		sc.Body = b.String()
	}
	if hv := resObj.Get("headers"); hv != nil && !goja.IsUndefined(hv) && !goja.IsNull(hv) {
		if hObj, ok3 := hv.(*goja.Object); ok3 {
			for _, k := range hObj.Keys() {
				v := hObj.Get(k)
				if v != nil && !goja.IsUndefined(v) {
					sc.Headers[k] = v.String()
				}
			}
		}
	}
	return sc, body, nil
}

func (h *HookEngine) RunOnResponse(reqMethod, reqURL string, statusCode *int, headers http.Header, body []byte) []byte {
	if h == nil {
		return body
	}
	for _, path := range h.files {
		body = h.runOnResponseFile(path, reqMethod, reqURL, statusCode, headers, body)
	}
	return body
}

func (h *HookEngine) runOnResponseFile(path, reqMethod, reqURL string, statusCode *int, headers http.Header, body []byte) []byte {
	src, err := os.ReadFile(path)
	if err != nil {
		h.log.Warn("hook read error", "file", path, "err", err)
		return body
	}
	rt := newRT(h.log)
	if _, err := rt.RunString(string(src)); err != nil {
		h.log.Warn("hook parse error", "file", path, "err", err)
		return body
	}
	fn, ok := getCallable(rt, "onResponse")
	if !ok {
		return body
	}

	reqObj := rt.NewObject()
	_ = reqObj.Set("method", reqMethod)
	_ = reqObj.Set("url", reqURL)

	resObj := rt.NewObject()
	_ = resObj.Set("status", *statusCode)
	_ = resObj.Set("headers", headersToObj(rt, headers))
	_ = resObj.Set("body", string(body))

	if _, err := fn(goja.Undefined(), reqObj, resObj); err != nil {
		h.log.Warn("hook onResponse error", "file", path, "err", err)
		return body
	}

	if s := resObj.Get("status"); s != nil && !goja.IsUndefined(s) {
		*statusCode = int(s.ToInteger())
	}
	if hv := resObj.Get("headers"); hv != nil && !goja.IsUndefined(hv) && !goja.IsNull(hv) {
		if hObj, ok2 := hv.(*goja.Object); ok2 {
			objToHeaders(hObj, headers)
		}
	}
	if b := resObj.Get("body"); b != nil && !goja.IsUndefined(b) {
		body = []byte(b.String())
	}
	return body
}

func (h *HookEngine) RunOnCapture(c *capture.Capture) {
	if h == nil {
		return
	}
	data, err := json.Marshal(c)
	if err != nil {
		return
	}
	var capMap map[string]interface{}
	if err := json.Unmarshal(data, &capMap); err != nil {
		return
	}
	for _, path := range h.files {
		h.runOnCaptureFile(path, capMap)
	}
}

func (h *HookEngine) runOnCaptureFile(path string, capMap map[string]interface{}) {
	src, err := os.ReadFile(path)
	if err != nil {
		return
	}
	rt := newRT(h.log)
	if _, err := rt.RunString(string(src)); err != nil {
		return
	}
	fn, ok := getCallable(rt, "onCapture")
	if !ok {
		return
	}
	if _, err := fn(goja.Undefined(), rt.ToValue(capMap)); err != nil {
		h.log.Warn("hook onCapture error", "file", path, "err", err)
	}
}
