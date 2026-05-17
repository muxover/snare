package web

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/muxover/snare/v2/capture"
	"github.com/muxover/snare/v2/intercept"
	"github.com/muxover/snare/v2/mock"
	sess "github.com/muxover/snare/v2/session"
)

type Server struct {
	Store     *capture.Store
	Mocks     *mock.Store
	Transport *http.Transport
	Intercept *intercept.Queue
	CADir     string
	WebPort   string
	ProxyAddr string // snare proxy address e.g. "127.0.0.1:8888"

	mu      sync.Mutex
	clients map[chan string]struct{}
}

func (s *Server) broadcast(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for ch := range s.clients {
		select {
		case ch <- msg:
		default:
		}
	}
}

func (s *Server) NotifyCapture(c *capture.Capture) {
	data, err := json.Marshal(c)
	if err != nil {
		return
	}
	s.broadcast("data: " + string(data) + "\n\n")
}

func (s *Server) notifyClear() {
	s.broadcast("event: cleared\ndata: {}\n\n")
}

func (s *Server) notifyDelete(id string) {
	s.broadcast("event: deleted\ndata: \"" + id + "\"\n\n")
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	sub, _ := fs.Sub(Static, "static")
	mux.Handle("/", http.FileServerFS(sub))
	mux.HandleFunc("/api/captures", s.handleCaptures)
	mux.HandleFunc("/api/captures/", s.handleCaptureByID)
	mux.HandleFunc("/api/mocks", s.handleMocks)
	mux.HandleFunc("/api/mocks/", s.handleMockByID)
	mux.HandleFunc("/api/export", s.handleExport)
	mux.HandleFunc("/api/intercept", s.handleIntercept)
	mux.HandleFunc("/api/intercept/", s.handleInterceptByID)
	mux.HandleFunc("/api/sessions", s.handleSessions)
	mux.HandleFunc("/api/sessions/", s.handleSessionByName)
	mux.HandleFunc("/api/events", s.sseEvents)
	mux.HandleFunc("/api/info", s.handleInfo)
	mux.HandleFunc("/ca.pem", s.handleCACert)
	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) handleCaptures(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		limitStr := r.URL.Query().Get("limit")
		limit := 500
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			limit = n
		}
		captures := s.Store.List(limit)
		if captures == nil {
			captures = []*capture.Capture{}
		}
		writeJSON(w, captures)
	case http.MethodDelete:
		s.Store.Clear(true)
		s.notifyClear()
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleCaptureByID(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/captures/")
	parts := strings.SplitN(path, "/", 2)
	id := parts[0]
	sub := ""
	if len(parts) > 1 {
		sub = parts[1]
	}

	switch {
	case r.Method == http.MethodGet && sub == "":
		c := s.Store.Get(id)
		if c == nil {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, c)

	case r.Method == http.MethodDelete && sub == "":
		if err := s.Store.DeleteByID(id); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		s.notifyDelete(id)
		w.WriteHeader(http.StatusNoContent)

	case r.Method == http.MethodPost && sub == "replay":
		c := s.Store.Get(id)
		if c == nil {
			http.NotFound(w, r)
			return
		}
		var edit struct {
			Method  string            `json:"method"`
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
			Body    string            `json:"body"`
		}
		_ = json.NewDecoder(r.Body).Decode(&edit)
		status, err := s.replayCapture(c, edit.Method, edit.URL, edit.Headers, edit.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		writeJSON(w, map[string]any{"status": status})

	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func (s *Server) replayCapture(c *capture.Capture, editMethod, editURL string, editHeaders map[string]string, editBody string) (int, error) {
	method := c.Request.Method
	if editMethod != "" {
		method = editMethod
	}
	rawURL := c.Request.URL
	if editURL != "" {
		rawURL = editURL
	}
	var body io.Reader
	if editBody != "" {
		body = strings.NewReader(editBody)
	} else if len(c.Request.Body) > 0 {
		body = strings.NewReader(string(c.Request.Body))
	}
	req, err := http.NewRequest(method, rawURL, body)
	if err != nil {
		return 0, err
	}
	if len(editHeaders) > 0 {
		for k, v := range editHeaders {
			req.Header.Set(k, v)
		}
	} else {
		for k, vs := range c.Request.Headers {
			for _, v := range vs {
				req.Header.Add(k, v)
			}
		}
	}

	// Route through the snare proxy so the replay is captured like any other request.
	var transport http.RoundTripper = s.Transport
	if s.ProxyAddr != "" {
		proxyURL, _ := url.Parse("http://" + s.ProxyAddr)
		transport = &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	}
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode, nil
}

func (s *Server) handleMocks(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rules := s.Mocks.Rules()
		if rules == nil {
			rules = []*mock.Rule{}
		}
		writeJSON(w, rules)

	case http.MethodDelete:
		if err := s.Mocks.Clear(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)

	case http.MethodPost:
		var input struct {
			Method      string `json:"method"`
			URLMatch    string `json:"url_match"`
			Status      int    `json:"status"`
			Body        string `json:"body"`
			ContentType string `json:"content_type"`
			Name        string `json:"name"`
		}
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if input.URLMatch == "" {
			http.Error(w, "url_match is required", http.StatusBadRequest)
			return
		}
		rule := &mock.Rule{
			ID:          uuid.New().String(),
			Name:        input.Name,
			Method:      strings.ToUpper(input.Method),
			URLPattern:  input.URLMatch,
			Status:      input.Status,
			Body:        input.Body,
			ContentType: input.ContentType,
		}
		if rule.Status == 0 {
			rule.Status = http.StatusOK
		}
		if err := s.Mocks.Add(rule); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, rule)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleMockByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/api/mocks/")
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ok, err := s.Mocks.Remove(id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	format := r.URL.Query().Get("format")
	captures := s.Store.ListFromDisk(0)
	if len(captures) == 0 {
		captures = s.Store.All()
	}

	switch format {
	case "openapi":
		w.Header().Set("Content-Disposition", `attachment; filename="openapi.json"`)
		w.Header().Set("Content-Type", "application/json")
		data, _ := json.MarshalIndent(buildWebOpenAPI(captures, "snare captured API"), "", "  ")
		_, _ = w.Write(data)
	case "har":
		w.Header().Set("Content-Disposition", `attachment; filename="export.har"`)
		w.Header().Set("Content-Type", "application/json")
		data, _ := json.MarshalIndent(buildWebHAR(captures), "", "  ")
		_, _ = w.Write(data)
	case "postman":
		w.Header().Set("Content-Disposition", `attachment; filename="export.postman_collection.json"`)
		w.Header().Set("Content-Type", "application/json")
		data, _ := json.MarshalIndent(buildWebPostman(captures), "", "  ")
		_, _ = w.Write(data)
	default:
		w.Header().Set("Content-Disposition", `attachment; filename="export.json"`)
		w.Header().Set("Content-Type", "application/json")
		data, _ := json.MarshalIndent(captures, "", "  ")
		_, _ = w.Write(data)
	}
}

func (s *Server) handleIntercept(w http.ResponseWriter, r *http.Request) {
	if s.Intercept == nil {
		writeJSON(w, []*intercept.PendingRequest{})
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pending, err := s.Intercept.Pending()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if pending == nil {
		pending = []*intercept.PendingRequest{}
	}
	writeJSON(w, pending)
}

func (s *Server) handleInterceptByID(w http.ResponseWriter, r *http.Request) {
	if s.Intercept == nil {
		http.Error(w, "intercept not enabled", http.StatusServiceUnavailable)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/intercept/")
	parts := strings.SplitN(path, "/", 2)
	id := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	switch action {
	case "forward":
		if err := s.Intercept.Decide(id, intercept.DecisionForward, nil); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)

	case "drop":
		if err := s.Intercept.Decide(id, intercept.DecisionDrop, nil); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)

	case "edit":
		var mod intercept.PendingRequest
		if err := json.NewDecoder(r.Body).Decode(&mod); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := s.Intercept.Decide(id, intercept.DecisionForward, &mod); err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)

	default:
		http.Error(w, "unknown action", http.StatusNotFound)
	}
}

func buildWebHAR(captures []*capture.Capture) map[string]any {
	entries := make([]map[string]any, 0, len(captures))
	for _, c := range captures {
		ent := map[string]any{
			"startedDateTime": c.Timestamp.Format(time.RFC3339),
			"time":            c.Duration.Milliseconds(),
			"request": map[string]any{
				"method":   c.Request.Method,
				"url":      c.Request.URL,
				"headers":  webHeadersToHAR(c.Request.Headers),
				"bodySize": len(c.Request.Body),
			},
		}
		if c.Response != nil {
			ent["response"] = map[string]any{
				"status":   c.Response.StatusCode,
				"headers":  webHeadersToHAR(c.Response.Headers),
				"bodySize": len(c.Response.Body),
			}
		}
		entries = append(entries, ent)
	}
	return map[string]any{
		"log": map[string]any{
			"version": "1.2",
			"creator": map[string]any{"name": "snare"},
			"entries": entries,
		},
	}
}

func webHeadersToHAR(h map[string][]string) []map[string]string {
	var out []map[string]string
	for k, v := range h {
		for _, vv := range v {
			out = append(out, map[string]string{"name": k, "value": vv})
		}
	}
	return out
}

func buildWebPostman(captures []*capture.Capture) map[string]any {
	items := make([]map[string]any, 0, len(captures))
	for _, c := range captures {
		u, err := url.Parse(c.Request.URL)
		if err != nil {
			continue
		}
		pathParts := strings.Split(strings.TrimPrefix(u.Path, "/"), "/")
		rawURL := map[string]any{
			"raw":      c.Request.URL,
			"protocol": u.Scheme,
			"host":     strings.Split(u.Host, "."),
			"path":     pathParts,
		}
		var headers []map[string]string
		for k, vals := range c.Request.Headers {
			for _, v := range vals {
				headers = append(headers, map[string]string{"key": k, "value": v})
			}
		}
		req := map[string]any{"method": c.Request.Method, "url": rawURL, "header": headers}
		if len(c.Request.Body) > 0 {
			req["body"] = map[string]any{"mode": "raw", "raw": webBodyString(c.Request.Body)}
		}
		items = append(items, map[string]any{"name": c.Request.Method + " " + u.Path, "request": req})
	}
	return map[string]any{
		"info": map[string]any{
			"name":   "snare export",
			"schema": "https://schema.getpostman.com/json/collection/v2.1.0/collection.json",
		},
		"item": items,
	}
}

func webBodyString(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	return base64.StdEncoding.EncodeToString(b)
}

func (s *Server) sseEvents(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	ch := make(chan string, 32)
	s.mu.Lock()
	if s.clients == nil {
		s.clients = make(map[chan string]struct{})
	}
	s.clients[ch] = struct{}{}
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		delete(s.clients, ch)
		s.mu.Unlock()
	}()

	fmt.Fprintf(w, ": connected\n\n")
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}

	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case msg := <-ch:
			fmt.Fprint(w, msg)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		case <-ticker.C:
			fmt.Fprintf(w, ": ping\n\n")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		case <-r.Context().Done():
			return
		}
	}
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sessions, err := sess.Load()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	all := s.Store.AllFromDisk()
	names := sess.SortedNames(sessions)
	type row struct {
		Name           string `json:"name"`
		StartFormatted string `json:"start_formatted"`
		EndFormatted   string `json:"end_formatted,omitempty"`
		Active         bool   `json:"active"`
		Count          int    `json:"count"`
	}
	out := make([]row, 0, len(names))
	for _, n := range names {
		e := sessions[n]
		caps := sess.Captures(all, e)
		r := row{
			Name:           n,
			StartFormatted: e.Start.Format("2006-01-02 15:04:05"),
			Active:         e.End.IsZero(),
			Count:          len(caps),
		}
		if !e.End.IsZero() {
			r.EndFormatted = e.End.Format("2006-01-02 15:04:05")
		}
		out = append(out, r)
	}
	writeJSON(w, out)
}

func (s *Server) handleSessionByName(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/sessions/")
	if path == "diff" {
		s.handleSessionDiff(w, r)
		return
	}
	parts := strings.SplitN(path, "/", 2)
	name := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}
	sessions, err := sess.Load()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if r.Method == http.MethodDelete && action == "" {
		delete(sessions, name)
		if err := sess.Save(sessions); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	switch action {
	case "start":
		sessions[name] = sess.Entry{Start: time.Now()}
		if err := sess.Save(sessions); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case "end":
		e, ok := sessions[name]
		if !ok {
			http.NotFound(w, r)
			return
		}
		e.End = time.Now()
		sessions[name] = e
		if err := sess.Save(sessions); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleSessionDiff(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	nameA := r.URL.Query().Get("a")
	nameB := r.URL.Query().Get("b")
	sessions, err := sess.Load()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	a, ok := sessions[nameA]
	if !ok {
		http.Error(w, "session not found: "+nameA, http.StatusNotFound)
		return
	}
	b, ok := sessions[nameB]
	if !ok {
		http.Error(w, "session not found: "+nameB, http.StatusNotFound)
		return
	}
	all := s.Store.AllFromDisk()
	seqA := sess.Captures(all, a)
	seqB := sess.Captures(all, b)

	type diffRow struct {
		Index int    `json:"index"`
		LineA string `json:"line_a"`
		LineB string `json:"line_b"`
		Match bool   `json:"match"`
	}
	n := len(seqA)
	if len(seqB) > n {
		n = len(seqB)
	}
	rows := make([]diffRow, 0, n)
	diffs := 0
	for i := 0; i < n; i++ {
		var lineA, lineB string
		if i < len(seqA) {
			c := seqA[i]
			lineA = fmt.Sprintf("%s %s %d", c.Request.Method, sess.RequestPath(c), sess.ResponseStatus(c))
		}
		if i < len(seqB) {
			c := seqB[i]
			lineB = fmt.Sprintf("%s %s %d", c.Request.Method, sess.RequestPath(c), sess.ResponseStatus(c))
		}
		match := lineA == lineB
		if !match {
			diffs++
		}
		rows = append(rows, diffRow{Index: i + 1, LineA: lineA, LineB: lineB, Match: match})
	}
	writeJSON(w, map[string]any{
		"a":       nameA,
		"b":       nameB,
		"count_a": len(seqA),
		"count_b": len(seqB),
		"diffs":   diffs,
		"rows":    rows,
	})
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	caAvailable := false
	if s.CADir != "" {
		if _, err := os.Stat(filepath.Join(s.CADir, "ca.pem")); err == nil {
			caAvailable = true
		}
	}
	writeJSON(w, map[string]any{
		"lan_ips":      lanIPs(),
		"web_port":     s.WebPort,
		"ca_available": caAvailable,
	})
}

func (s *Server) handleCACert(w http.ResponseWriter, r *http.Request) {
	if s.CADir == "" {
		http.Error(w, "CA not configured", http.StatusNotFound)
		return
	}
	certPath := filepath.Join(s.CADir, "ca.pem")
	data, err := os.ReadFile(certPath)
	if err != nil {
		http.Error(w, "CA certificate not found — run 'snare ca generate' first", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/x-x509-ca-cert")
	w.Header().Set("Content-Disposition", "attachment; filename=snare-ca.pem")
	_, _ = w.Write(data)
}

func lanIPs() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil {
				out = append(out, ipnet.IP.String())
			}
		}
	}
	return out
}

var (
	reOAUUID = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	reOANum  = regexp.MustCompile(`^\d+$`)
)

func buildWebOpenAPI(captures []*capture.Capture, title string) map[string]any {
	type opKey struct{ path, method string }
	type opVal struct {
		ct, reqBody          string
		status               int
		respCT, respBody     string
	}
	ops := make(map[opKey]*opVal)
	serverURL := ""

	normPath := func(path string) string {
		parts := strings.Split(path, "/")
		for i, p := range parts {
			if reOAUUID.MatchString(p) || reOANum.MatchString(p) {
				parts[i] = "{id}"
			}
		}
		return strings.Join(parts, "/")
	}

	for _, c := range captures {
		if c.Request.URL == "" {
			continue
		}
		u, err := url.Parse(c.Request.URL)
		if err != nil {
			continue
		}
		if serverURL == "" {
			serverURL = u.Scheme + "://" + u.Host
		}
		k := opKey{path: normPath(u.Path), method: strings.ToLower(c.Request.Method)}
		if _, exists := ops[k]; exists {
			continue
		}
		o := &opVal{}
		if len(c.Request.Body) > 0 {
			o.ct = c.Request.Headers.Get("Content-Type")
			o.reqBody = string(c.Request.Body)
		}
		if c.Response != nil {
			o.status = c.Response.StatusCode
			o.respCT = c.Response.Headers.Get("Content-Type")
			o.respBody = string(c.Response.Body)
		}
		ops[k] = o
	}

	paths := make(map[string]any)
	for k, o := range ops {
		if paths[k.path] == nil {
			paths[k.path] = make(map[string]any)
		}
		operation := map[string]any{}
		if o.reqBody != "" {
			ct := o.ct
			if ct == "" {
				ct = "application/json"
			}
			operation["requestBody"] = map[string]any{
				"content": map[string]any{ct: map[string]any{"example": o.reqBody}},
			}
		}
		statusStr := "200"
		if o.status > 0 {
			statusStr = strconv.Itoa(o.status)
		}
		resp := map[string]any{"description": http.StatusText(o.status)}
		if o.respBody != "" {
			ct := o.respCT
			if ct == "" {
				ct = "application/json"
			}
			resp["content"] = map[string]any{ct: map[string]any{"example": o.respBody}}
		}
		operation["responses"] = map[string]any{statusStr: resp}
		paths[k.path].(map[string]any)[k.method] = operation
	}

	return map[string]any{
		"openapi": "3.0.3",
		"info":    map[string]any{"title": title, "version": "0.1.0"},
		"servers": []map[string]any{{"url": serverURL}},
		"paths":   paths,
	}
}
