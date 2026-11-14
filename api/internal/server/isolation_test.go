package server

import (
	"net/http"
	"testing"
)

type fixture struct {
	project, task, logEntry, tag, session string
}

func seedUser(t *testing.T, e *env, email, pw, ip, tagName string) (string, fixture) {
	t.Helper()
	e.createUser(email, pw)
	tok := e.login(email, pw, ip)
	var p struct {
		ID string `json:"id"`
	}
	rec := e.do(req{method: "POST", path: "/v1/projects", token: tok,
		body: map[string]any{"name": "Secret " + email, "description": "private", "tags": []string{tagName}}})
	e.mustStatus(rec, http.StatusCreated)
	decodeBody(t, rec, &p)
	var task, entry struct {
		ID string `json:"id"`
	}
	rec = e.do(req{method: "POST", path: "/v1/projects/" + p.ID + "/tasks", token: tok, body: map[string]string{"title": "task"}})
	e.mustStatus(rec, http.StatusCreated)
	decodeBody(t, rec, &task)
	rec = e.do(req{method: "POST", path: "/v1/projects/" + p.ID + "/log", token: tok, body: map[string]string{"body": "entry"}})
	e.mustStatus(rec, http.StatusCreated)
	decodeBody(t, rec, &entry)
	var tags struct {
		Tags []struct {
			ID string `json:"id"`
		} `json:"tags"`
	}
	rec = e.do(req{method: "GET", path: "/v1/tags", token: tok})
	decodeBody(t, rec, &tags)
	var sessions struct {
		Sessions []struct {
			ID string `json:"id"`
		} `json:"sessions"`
	}
	rec = e.do(req{method: "GET", path: "/v1/auth/sessions", token: tok})
	decodeBody(t, rec, &sessions)
	return tok, fixture{project: p.ID, task: task.ID, logEntry: entry.ID, tag: tags.Tags[0].ID, session: sessions.Sessions[0].ID}
}

// TestCrossUserIsolation checks that user B cannot read, change, delete or
// attach anything of user A's through any endpoint, and that every attempt
// is a 404 (never 403).
func TestCrossUserIsolation(t *testing.T) {
	e := newEnv(t)
	tokA, a := seedUser(t, e, "a@example.com", pwA, "192.0.2.1", "Freelance")
	tokB, b := seedUser(t, e, "b@example.com", pwB, "192.0.2.2", "Personal")

	cases := []struct {
		name, method, path string
		body               any
		want               int
	}{
		{"get project", "GET", "/v1/projects/" + a.project, nil, 404},
		{"patch project", "PATCH", "/v1/projects/" + a.project, map[string]any{"name": "pwned"}, 404},
		{"archive project", "PATCH", "/v1/projects/" + a.project, map[string]any{"archived": true}, 404},
		{"retag project", "PATCH", "/v1/projects/" + a.project, map[string]any{"tags": []string{"x"}}, 404},
		{"delete project", "DELETE", "/v1/projects/" + a.project, nil, 404},
		{"add log to project", "POST", "/v1/projects/" + a.project + "/log", map[string]string{"body": "pwned"}, 404},
		{"add task to project", "POST", "/v1/projects/" + a.project + "/tasks", map[string]string{"title": "pwned"}, 404},
		{"patch log", "PATCH", "/v1/log/" + a.logEntry, map[string]string{"body": "pwned"}, 404},
		{"delete log", "DELETE", "/v1/log/" + a.logEntry, nil, 404},
		{"patch task", "PATCH", "/v1/tasks/" + a.task, map[string]any{"done": true, "title": "pwned"}, 404},
		{"delete task", "DELETE", "/v1/tasks/" + a.task, nil, 404},
		{"rename tag", "PATCH", "/v1/tags/" + a.tag, map[string]string{"name": "pwned"}, 404},
		{"revoke session", "DELETE", "/v1/auth/sessions/" + a.session, nil, 404},
		// Reordering with A's project id must fail without revealing it exists.
		{"reorder with foreign id", "PATCH", "/v1/projects/order", map[string][]string{"ids": {b.project, a.project}}, 400},
		{"reorder only foreign id", "PATCH", "/v1/projects/order", map[string][]string{"ids": {a.project}}, 400},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := e.do(req{method: c.method, path: c.path, token: tokB, body: c.body})
			e.mustStatus(rec, c.want)
		})
	}

	// Attaching: B using A's tag name gets B's own new tag, not A's.
	rec := e.do(req{method: "PATCH", path: "/v1/projects/" + b.project, token: tokB, body: map[string]any{"tags": []string{"freelance"}}})
	e.mustStatus(rec, http.StatusOK)
	var bp struct {
		Tags []string `json:"tags"`
	}
	decodeBody(t, rec, &bp)
	if len(bp.Tags) != 1 || bp.Tags[0] != "freelance" {
		t.Fatalf("B's tags %v (expected B's own spelling)", bp.Tags)
	}

	// Listings never include the other user's data.
	var list struct {
		Projects []struct {
			ID string `json:"id"`
		} `json:"projects"`
	}
	rec = e.do(req{method: "GET", path: "/v1/projects", token: tokB})
	decodeBody(t, rec, &list)
	if len(list.Projects) != 1 || list.Projects[0].ID != b.project {
		t.Fatalf("B sees %+v", list)
	}
	var tags struct {
		Tags []struct {
			ID           string `json:"id"`
			Name         string `json:"name"`
			ProjectCount int    `json:"project_count"`
		} `json:"tags"`
	}
	rec = e.do(req{method: "GET", path: "/v1/tags", token: tokB})
	decodeBody(t, rec, &tags)
	for _, tg := range tags.Tags {
		if tg.ID == a.tag {
			t.Fatal("B sees A's tag")
		}
	}
	var sessions struct {
		Sessions []struct {
			ID string `json:"id"`
		} `json:"sessions"`
	}
	rec = e.do(req{method: "GET", path: "/v1/auth/sessions", token: tokB})
	decodeBody(t, rec, &sessions)
	for _, s := range sessions.Sessions {
		if s.ID == a.session {
			t.Fatal("B sees A's session")
		}
	}

	// A's data is untouched.
	var full struct {
		Name       string   `json:"name"`
		Tags       []string `json:"tags"`
		ArchivedAt *string  `json:"archived_at"`
		Tasks      []struct {
			Title string `json:"title"`
			Done  bool   `json:"done"`
		} `json:"tasks"`
		Log []struct {
			Body string `json:"body"`
		} `json:"log"`
	}
	rec = e.do(req{method: "GET", path: "/v1/projects/" + a.project, token: tokA})
	e.mustStatus(rec, http.StatusOK)
	decodeBody(t, rec, &full)
	if full.Name != "Secret a@example.com" || full.ArchivedAt != nil || len(full.Tags) != 1 || full.Tags[0] != "Freelance" ||
		len(full.Tasks) != 1 || full.Tasks[0].Title != "task" || full.Tasks[0].Done ||
		len(full.Log) != 1 || full.Log[0].Body != "entry" {
		t.Fatalf("A's project changed: %+v", full)
	}
	rec = e.do(req{method: "GET", path: "/v1/tags", token: tokA})
	decodeBody(t, rec, &tags)
	if len(tags.Tags) != 1 || tags.Tags[0].Name != "Freelance" || tags.Tags[0].ProjectCount != 1 {
		t.Fatalf("A's tags changed: %+v", tags)
	}
	e.mustStatus(e.do(req{method: "GET", path: "/v1/projects", token: tokA}), http.StatusOK)
}
