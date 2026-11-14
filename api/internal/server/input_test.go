package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func authed(t *testing.T) (*env, string, string) {
	t.Helper()
	e := newEnv(t)
	e.createUser("a@example.com", pwA)
	tok := e.login("a@example.com", pwA, "")
	rec := e.do(req{method: "POST", path: "/v1/projects", token: tok, body: map[string]any{"name": "P"}})
	e.mustStatus(rec, http.StatusCreated)
	var p struct {
		ID string `json:"id"`
	}
	decodeBody(t, rec, &p)
	return e, tok, p.ID
}

func TestOversizedBodies(t *testing.T) {
	e, tok, pid := authed(t)
	big := func(n int) string { return `{"name":"x","description":"` + strings.Repeat("a", n) + `"}` }

	// 1 MB cap on project and log routes.
	e.mustStatus(e.do(req{method: "POST", path: "/v1/projects", token: tok, body: big(1 << 20)}), http.StatusRequestEntityTooLarge)
	e.mustStatus(e.do(req{method: "PATCH", path: "/v1/projects/" + pid, token: tok, body: big(1 << 20)}), http.StatusRequestEntityTooLarge)
	e.mustStatus(e.do(req{method: "POST", path: "/v1/projects/" + pid + "/log", token: tok,
		body: `{"body":"` + strings.Repeat("a", 1<<20) + `"}`}), http.StatusRequestEntityTooLarge)
	// 64 KB default elsewhere.
	e.mustStatus(e.do(req{method: "POST", path: "/v1/projects/" + pid + "/tasks", token: tok,
		body: `{"title":"` + strings.Repeat("a", 65<<10) + `"}`}), http.StatusRequestEntityTooLarge)
	e.mustStatus(e.do(req{method: "POST", path: "/v1/auth/login",
		body: `{"email":"` + strings.Repeat("a", 65<<10) + `"}`}), http.StatusRequestEntityTooLarge)

	// Body cap is enforced even without a Content-Length header (chunked).
	hr := httptest.NewRequest("POST", "/v1/projects/"+pid+"/tasks", struct{ *strings.Reader }{strings.NewReader(`{"title":"` + strings.Repeat("a", 70<<10) + `"}`)})
	hr.ContentLength = -1
	hr.RemoteAddr = "198.51.100.7:1"
	hr.Header.Set("Content-Type", "application/json")
	hr.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	e.srv.ServeHTTP(rec, hr)
	e.mustStatus(rec, http.StatusRequestEntityTooLarge)

	// Field limits: 100,000 characters are accepted, 100,001 rejected.
	e.mustStatus(e.do(req{method: "PATCH", path: "/v1/projects/" + pid, token: tok,
		body: map[string]string{"description": strings.Repeat("é", 100_000)}}), http.StatusOK)
	e.mustStatus(e.do(req{method: "PATCH", path: "/v1/projects/" + pid, token: tok,
		body: map[string]string{"description": strings.Repeat("a", 100_001)}}), http.StatusBadRequest)
	e.mustStatus(e.do(req{method: "POST", path: "/v1/projects/" + pid + "/log", token: tok,
		body: map[string]string{"body": strings.Repeat("a", 100_001)}}), http.StatusBadRequest)
}

func TestStrictJSON(t *testing.T) {
	e, tok, pid := authed(t)
	path := "/v1/projects/" + pid + "/tasks"
	cases := []struct {
		name, body, ct string
		want           int
	}{
		{"unknown field", `{"title":"x","admin":true}`, "", 400},
		{"two objects", `{"title":"x"}{"title":"y"}`, "", 400},
		{"trailing garbage", `{"title":"x"} x`, "", 400},
		{"array", `[{"title":"x"}]`, "", 400},
		{"null", `null`, "", 400},
		{"string", `"x"`, "", 400},
		{"empty", ``, "", 400},
		{"malformed", `{"title":`, "", 400},
		{"wrong type", `{"title":5}`, "", 400},
		{"no content type", `{"title":"x"}`, "-", 415},
		{"text/plain", `{"title":"x"}`, "text/plain", 415},
		{"form", `title=x`, "application/x-www-form-urlencoded", 415},
		{"bad charset", `{"title":"x"}`, "application/json; charset=latin1", 415},
		{"ok with charset", `{"title":"x"}`, "application/json; charset=utf-8", 201},
		{"ok", `{"title":"x"}  `, "", 201},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := e.do(req{method: "POST", path: path, token: tok, body: c.body, contentType: c.ct})
			e.mustStatus(rec, c.want)
		})
	}
	// Unknown fields are rejected on every JSON endpoint.
	for _, p := range []struct{ method, path string }{
		{"POST", "/v1/auth/login"}, {"POST", "/v1/auth/password"}, {"POST", "/v1/projects"},
		{"PATCH", "/v1/projects/" + pid}, {"PATCH", "/v1/projects/order"}, {"POST", "/v1/projects/" + pid + "/log"},
	} {
		rec := e.do(req{method: p.method, path: p.path, token: tok, body: `{"user_id":"x"}`})
		e.mustStatus(rec, http.StatusBadRequest)
	}
}

func TestMalformedIDs(t *testing.T) {
	e, tok, _ := authed(t)
	bad := []string{"not-a-uuid", "6F9619FF-8B86-D011-B42D-00C04FC964FF", "1%27%20OR%201%3D1", "00000000-0000-0000-0000-00000000000g", "..%2f..%2fetc"}
	routes := []struct{ method, path string }{
		{"GET", "/v1/projects/%s"}, {"PATCH", "/v1/projects/%s"}, {"DELETE", "/v1/projects/%s"},
		{"POST", "/v1/projects/%s/log"}, {"POST", "/v1/projects/%s/tasks"},
		{"PATCH", "/v1/log/%s"}, {"DELETE", "/v1/log/%s"},
		{"PATCH", "/v1/tasks/%s"}, {"DELETE", "/v1/tasks/%s"},
		{"PATCH", "/v1/tags/%s"}, {"DELETE", "/v1/auth/sessions/%s"},
	}
	for _, r := range routes {
		for _, id := range bad {
			var body any
			if r.method == "PATCH" || r.method == "POST" {
				body = map[string]string{}
			}
			rec := e.do(req{method: r.method, path: strings.Replace(r.path, "%s", id, 1), token: tok, body: body})
			if rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
				t.Fatalf("%s %s with %q: %d", r.method, r.path, id, rec.Code)
			}
			if rec.Code == http.StatusBadRequest && errorMessage(t, rec) != "invalid id" {
				t.Fatalf("%s %s with %q: %s", r.method, r.path, id, rec.Body.String())
			}
		}
	}
	// Well-formed but unknown ids are 404.
	e.mustStatus(e.do(req{method: "GET", path: "/v1/projects/00000000-0000-4000-8000-000000000000", token: tok}), http.StatusNotFound)
	// Malformed ids inside a reorder body.
	e.mustStatus(e.do(req{method: "PATCH", path: "/v1/projects/order", token: tok, body: map[string][]string{"ids": {"x"}}}), http.StatusBadRequest)
}

func TestFieldValidation(t *testing.T) {
	e, tok, pid := authed(t)
	tooMany := make([]string, 21)
	for i := range tooMany {
		tooMany[i] = strings.Repeat("t", i+1)
	}
	twenty := tooMany[:20]
	cases := []struct {
		name string
		body any
		want int
	}{
		{"empty name", map[string]any{"name": "   "}, 400},
		{"name 200", map[string]any{"name": strings.Repeat("n", 200)}, 200},
		{"name 201", map[string]any{"name": strings.Repeat("n", 201)}, 400},
		{"name newline", map[string]any{"name": "a\nb"}, 400},
		{"name control", map[string]any{"name": "a\x00b"}, 400},
		{"description control", map[string]any{"description": "a\x07b"}, 400},
		{"description newlines ok", map[string]any{"description": "para one\r\n\r\npara two\ttab"}, 200},
		{"20 tags", map[string]any{"tags": twenty}, 200},
		{"21 tags", map[string]any{"tags": tooMany}, 400},
		{"21 tags with duplicates is 20", map[string]any{"tags": append(append([]string{}, twenty...), "T")}, 200},
		{"empty tag", map[string]any{"tags": []string{" "}}, 400},
		{"tag 40", map[string]any{"tags": []string{strings.Repeat("x", 40)}}, 200},
		{"tag 41", map[string]any{"tags": []string{strings.Repeat("x", 41)}}, 400},
		{"tag control", map[string]any{"tags": []string{"a\tb"}}, 400},
		{"archived wrong type", map[string]any{"archived": "yes"}, 400},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e.mustStatus(e.do(req{method: "PATCH", path: "/v1/projects/" + pid, token: tok, body: c.body}), c.want)
		})
	}
	// Line breaks are normalised and preserved.
	rec := e.do(req{method: "GET", path: "/v1/projects/" + pid, token: tok})
	var p struct {
		Description string   `json:"description"`
		Tags        []string `json:"tags"`
	}
	decodeBody(t, rec, &p)
	if p.Description != "para one\n\npara two\ttab" {
		t.Fatalf("description %q", p.Description)
	}
	// Tags trimmed.
	rec = e.do(req{method: "PATCH", path: "/v1/projects/" + pid, token: tok, body: map[string]any{"tags": []string{"  Mobile App  "}}})
	decodeBody(t, rec, &p)
	if len(p.Tags) != 1 || p.Tags[0] != "Mobile App" {
		t.Fatalf("tags %v", p.Tags)
	}
	e.mustStatus(e.do(req{method: "POST", path: "/v1/projects/" + pid + "/tasks", token: tok, body: map[string]string{"title": strings.Repeat("t", 501)}}), 400)
	e.mustStatus(e.do(req{method: "POST", path: "/v1/projects/" + pid + "/tasks", token: tok, body: map[string]string{"title": strings.Repeat("t", 500)}}), 201)
	e.mustStatus(e.do(req{method: "POST", path: "/v1/projects/" + pid + "/log", token: tok, body: map[string]string{"body": " \n "}}), 400)
}

func TestSecurityHeadersEverywhere(t *testing.T) {
	e, tok, _ := authed(t)
	limited := newEnv(t, func(o *Options) { o.RatePerMinute, o.RateBurst = 1, 1 })
	recs := map[string]*httptest.ResponseRecorder{
		"200": e.do(req{method: "GET", path: "/v1/projects", token: tok}),
		"401": e.do(req{method: "GET", path: "/v1/projects"}),
		"404": e.do(req{method: "GET", path: "/nope"}),
		"404m": e.do(req{method: "PUT", path: "/v1/projects"}),
		"400": e.do(req{method: "POST", path: "/v1/projects", token: tok, body: `{"x":1}`}),
	}
	limited.do(req{method: "GET", path: "/v1/healthz"})
	recs["429"] = limited.do(req{method: "GET", path: "/v1/healthz"})
	want := map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"Cache-Control":           "no-store",
		"Referrer-Policy":         "no-referrer",
		"Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'",
		"X-Frame-Options":         "DENY",
	}
	for code, rec := range recs {
		if rec.Code != map[string]int{"200": 200, "401": 401, "404": 404, "404m": 404, "400": 400, "429": 429}[code] {
			t.Fatalf("%s: got %d", code, rec.Code)
		}
		for k, v := range want {
			if got := rec.Header().Get(k); got != v {
				t.Fatalf("%s response: %s = %q", code, k, got)
			}
		}
		for k := range rec.Header() {
			if strings.HasPrefix(strings.ToLower(k), "access-control-") {
				t.Fatalf("%s response has CORS header %s", code, k)
			}
		}
	}
	// A browser preflight gets no CORS approval.
	rec := e.do(req{method: "OPTIONS", path: "/v1/projects", header: map[string]string{"Origin": "https://evil.example", "Access-Control-Request-Method": "GET"}})
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("preflight approved")
	}
}

func TestHealthzRevealsNothing(t *testing.T) {
	e := newEnv(t)
	rec := e.do(req{method: "GET", path: "/v1/healthz"})
	e.mustStatus(rec, http.StatusOK)
	if strings.TrimSpace(rec.Body.String()) != `{"status":"ok"}` || rec.Header().Get("Server") != "" {
		t.Fatalf("healthz leaks: %q %v", rec.Body.String(), rec.Header())
	}
}

func TestGlobalRateLimit(t *testing.T) {
	e := newEnv(t, func(o *Options) { o.RatePerMinute, o.RateBurst = 60, 5 })
	e.createUser("a@example.com", pwA)
	tok := e.login("a@example.com", pwA, "192.0.2.1")
	// Per IP.
	for i := 0; i < 4; i++ { // the login above used one token
		e.mustStatus(e.do(req{method: "GET", path: "/v1/healthz", ip: "192.0.2.1"}), 200)
	}
	rec := e.do(req{method: "GET", path: "/v1/healthz", ip: "192.0.2.1"})
	e.mustStatus(rec, http.StatusTooManyRequests)
	if rec.Header().Get("Retry-After") != "1" {
		t.Fatalf("Retry-After %q", rec.Header().Get("Retry-After"))
	}
	// Per session, across different IPs.
	for i := 0; i < 5; i++ {
		e.mustStatus(e.do(req{method: "GET", path: "/v1/tags", token: tok, ip: "192.0.3." + string(rune('1'+i))}), 200)
	}
	rec = e.do(req{method: "GET", path: "/v1/tags", token: tok, ip: "192.0.3.9"})
	e.mustStatus(rec, http.StatusTooManyRequests)
	e.clock.Add(2 * time.Second)
	e.mustStatus(e.do(req{method: "GET", path: "/v1/tags", token: tok, ip: "192.0.3.9"}), 200)
}

func TestClientIP(t *testing.T) {
	e := newEnv(t)
	ip := func(remote string, xff ...string) string {
		hr := httptest.NewRequest("GET", "/", nil)
		hr.RemoteAddr = remote
		for _, v := range xff {
			hr.Header.Add("X-Forwarded-For", v)
		}
		return e.srv.clientIP(hr)
	}
	if got := ip("10.0.0.2:5000", "203.0.113.9"); got != "203.0.113.9" {
		t.Fatalf("trusted proxy XFF ignored: %s", got)
	}
	if got := ip("10.0.0.2:5000", "1.1.1.1, 203.0.113.9"); got != "203.0.113.9" {
		t.Fatalf("spoofed leftmost XFF used: %s", got)
	}
	if got := ip("198.51.100.1:5000", "203.0.113.9"); got != "198.51.100.1" {
		t.Fatalf("XFF from untrusted peer honoured: %s", got)
	}
	if got := ip("10.0.0.2:5000", "garbage"); got != "10.0.0.2" {
		t.Fatalf("garbage XFF: %s", got)
	}
	if got := ip("[::ffff:10.0.0.2]:5000", "203.0.113.9"); got != "203.0.113.9" {
		t.Fatalf("mapped proxy address: %s", got)
	}
}

func TestPanicIsRecoveredAndGeneric(t *testing.T) {
	e := newEnv(t)
	h := securityHeaders(e.srv.recoverPanics(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("db exploded at /secret/path")
	})))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	e.mustStatus(rec, http.StatusInternalServerError)
	if strings.Contains(rec.Body.String(), "secret") || errorMessage(t, rec) != "internal error" {
		t.Fatalf("panic leaked: %s", rec.Body.String())
	}
	if !strings.Contains(e.logs.String(), "db exploded") {
		t.Fatal("panic not logged")
	}
}

func TestLogsOmitSecrets(t *testing.T) {
	e := newEnv(t)
	e.createUser("a@example.com", pwA)
	tok := e.login("a@example.com", pwA, "")
	e.do(req{method: "POST", path: "/v1/auth/password", token: tok, body: map[string]string{"current_password": pwA, "new_password": pwB}})
	e.do(req{method: "POST", path: "/v1/auth/login", body: map[string]string{"email": "a@example.com", "password": "my wrong password"}})
	logs := e.logs.String()
	for _, secret := range []string{tok, pwA, pwB, "my wrong password", "Bearer"} {
		if strings.Contains(logs, secret) {
			t.Fatalf("log contains %q", secret)
		}
	}
}

// TestFullFlow exercises the happy paths of every endpoint.
func TestFullFlow(t *testing.T) {
	e, tok, p1 := authed(t)
	call := func(method, path string, body any, want int, out any) {
		t.Helper()
		rec := e.do(req{method: method, path: path, token: tok, body: body})
		e.mustStatus(rec, want)
		if out != nil {
			decodeBody(t, rec, out)
		}
	}
	type project struct {
		ID         string   `json:"id"`
		Name       string   `json:"name"`
		Tags       []string `json:"tags"`
		ArchivedAt *string  `json:"archived_at"`
		Tasks      []struct {
			ID          string  `json:"id"`
			Done        bool    `json:"done"`
			CompletedAt *string `json:"completed_at"`
		} `json:"tasks"`
		Log []struct {
			ID   string `json:"id"`
			Body string `json:"body"`
		} `json:"log"`
	}
	var p2 project
	call("POST", "/v1/projects", map[string]any{"name": "Second", "description": "d", "tags": []string{"Mobile App", "mobile app", "Freelance"}}, 201, &p2)
	if len(p2.Tags) != 2 {
		t.Fatalf("tags %v", p2.Tags)
	}
	var task struct {
		ID          string  `json:"id"`
		Title       string  `json:"title"`
		Done        bool    `json:"done"`
		CompletedAt *string `json:"completed_at"`
	}
	call("POST", "/v1/projects/"+p2.ID+"/tasks", map[string]string{"title": "Do it"}, 201, &task)
	call("PATCH", "/v1/tasks/"+task.ID, map[string]any{"done": true}, 200, &task)
	if !task.Done || task.CompletedAt == nil {
		t.Fatalf("task %+v", task)
	}
	call("PATCH", "/v1/tasks/"+task.ID, map[string]any{"done": false, "title": "Renamed"}, 200, &task)
	if task.Done || task.CompletedAt != nil || task.Title != "Renamed" {
		t.Fatalf("task %+v", task)
	}
	for _, body := range []string{"one", "two", "three"} {
		call("POST", "/v1/projects/"+p2.ID+"/log", map[string]string{"body": body}, 201, nil)
		e.clock.Add(time.Second)
	}
	var list struct {
		Projects []project `json:"projects"`
	}
	call("GET", "/v1/projects", nil, 200, &list)
	if len(list.Projects) != 2 || list.Projects[1].ID != p2.ID || len(list.Projects[1].Log) != 2 || list.Projects[1].Log[0].Body != "three" || len(list.Projects[1].Tasks) != 1 {
		t.Fatalf("board %+v", list)
	}
	var full project
	call("GET", "/v1/projects/"+p2.ID, nil, 200, &full)
	if len(full.Log) != 3 {
		t.Fatalf("full log %+v", full.Log)
	}
	var entry struct {
		Body string `json:"body"`
	}
	call("PATCH", "/v1/log/"+full.Log[2].ID, map[string]string{"body": "first, edited"}, 200, &entry)
	if entry.Body != "first, edited" {
		t.Fatal("log not edited")
	}
	call("DELETE", "/v1/log/"+full.Log[2].ID, nil, 204, nil)
	call("DELETE", "/v1/tasks/"+task.ID, nil, 204, nil)

	call("PATCH", "/v1/projects/order", map[string][]string{"ids": {p2.ID, p1}}, 204, nil)
	call("GET", "/v1/projects", nil, 200, &list)
	if list.Projects[0].ID != p2.ID {
		t.Fatal("order not applied")
	}

	var tags struct {
		Tags []struct {
			ID           string `json:"id"`
			Name         string `json:"name"`
			ProjectCount int    `json:"project_count"`
		} `json:"tags"`
	}
	call("PATCH", "/v1/projects/"+p1, map[string]any{"tags": []string{"freelance"}}, 200, nil)
	call("GET", "/v1/tags", nil, 200, &tags)
	if len(tags.Tags) != 2 || tags.Tags[0].Name != "Freelance" || tags.Tags[0].ProjectCount != 2 {
		t.Fatalf("tags %+v", tags)
	}
	var renamed struct {
		Name string `json:"name"`
	}
	call("PATCH", "/v1/tags/"+tags.Tags[1].ID, map[string]string{"name": "freelance"}, 409, nil)
	call("PATCH", "/v1/tags/"+tags.Tags[0].ID, map[string]string{"name": "Contract work"}, 200, &renamed)
	call("GET", "/v1/projects/"+p1, nil, 200, &full)
	if len(full.Tags) != 1 || full.Tags[0] != "Contract work" {
		t.Fatalf("rename not applied everywhere: %v", full.Tags)
	}

	call("PATCH", "/v1/projects/"+p1, map[string]any{"archived": true}, 200, &full)
	if full.ArchivedAt == nil {
		t.Fatal("not archived")
	}
	call("PATCH", "/v1/projects/"+p1, map[string]any{"archived": false}, 200, &full)
	if full.ArchivedAt != nil {
		t.Fatal("not restored")
	}
	call("DELETE", "/v1/projects/"+p2.ID, nil, 204, nil)
	call("GET", "/v1/projects/"+p2.ID, nil, 404, nil)
	call("GET", "/v1/tags", nil, 200, &tags)
	if len(tags.Tags) != 1 {
		t.Fatalf("orphan tags not removed: %+v", tags)
	}

	// Responses are JSON with tags always an array (never null).
	rec := e.do(req{method: "POST", path: "/v1/projects", token: tok, body: map[string]any{"name": "No tags"}})
	var raw map[string]json.RawMessage
	_ = json.NewDecoder(bytes.NewReader(rec.Body.Bytes())).Decode(&raw)
	if string(raw["tags"]) != "[]" || string(raw["tasks"]) != "[]" || string(raw["log"]) != "[]" {
		t.Fatalf("arrays not empty-array: %s", rec.Body.String())
	}
}
