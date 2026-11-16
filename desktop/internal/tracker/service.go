// Package tracker is the service bound to the frontend. Every API call is
// made here, on the Go side; the session token is read from the keychain and
// never returned to JavaScript.
package tracker

import (
	"context"
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"
	"unicode/utf8"

	"tracker/desktop/internal/api"
	"tracker/desktop/internal/brand"
	"tracker/desktop/internal/config"
	"tracker/desktop/internal/secrets"
	"tracker/desktop/internal/serverurl"
)

// Service is bound to the frontend.
type Service struct {
	cfg      *config.Store
	keychain secrets.Keychain
	newAPI   func(base string, insecureTLS bool) *api.Client

	mu     sync.Mutex
	server string
	token  string
	client *api.Client
}

// New returns a Service, restoring the session from the keychain if any.
func New(cfg *config.Store, kc secrets.Keychain) *Service {
	s := &Service{cfg: cfg, keychain: kc, newAPI: api.New}
	if c, err := cfg.Load(); err == nil && c.Server != "" {
		if origin, err := serverurl.Normalize(c.Server); err == nil {
			if tok, err := kc.Get(origin); err == nil && tok != "" {
				s.server, s.token, s.client = origin, tok, api.New(origin, c.InsecureTLS)
			}
		}
	}
	return s
}

// Status describes the connection for the frontend.
type Status struct {
	AppName     string `json:"app_name"`
	Connected   bool   `json:"connected"`
	Server      string `json:"server"`
	Host        string `json:"host"`
	Email       string `json:"email"`
	Device      string `json:"device"`
	InsecureTLS bool   `json:"insecure_tls"`
}

// Status returns the app name and whether a session is stored. It also
// returns the last-used server and email so the Connect form can be prefilled.
func (s *Service) Status() Status {
	c, _ := s.cfg.Load()
	s.mu.Lock()
	defer s.mu.Unlock()
	st := Status{AppName: brand.Name, Connected: s.token != "", Server: c.Server, Email: c.Email, Device: deviceName(), InsecureTLS: c.InsecureTLS}
	if s.server != "" {
		st.Server = s.server
	}
	if st.Server != "" {
		st.Host = serverurl.Host(st.Server)
	}
	return st
}

func deviceName() string {
	h, err := os.Hostname()
	if err != nil || strings.TrimSpace(h) == "" {
		return "Unknown device"
	}
	h = strings.TrimSuffix(strings.TrimSpace(h), ".local")
	if utf8.RuneCountInString(h) > 100 {
		h = string([]rune(h)[:100])
	}
	return h
}

// Connect signs in to server and stores the session token in the keychain.
// insecureTLS disables certificate verification for this server (the
// "Disable SSL certificate verification" option on the Connect screen).
func (s *Service) Connect(ctx context.Context, server, email, password string, insecureTLS bool) error {
	origin, err := serverurl.Normalize(server)
	if err != nil {
		return &api.Error{Kind: api.KindInvalid, Message: err.Error()}
	}
	email = strings.TrimSpace(email)
	if email == "" || password == "" {
		return &api.Error{Kind: api.KindInvalid, Message: "enter your email and password"}
	}
	client := s.newAPI(origin, insecureTLS)
	var out struct {
		Token string `json:"token"`
	}
	err = client.Do(ctx, "POST", "/v1/auth/login", "", map[string]string{
		"email": email, "password": password, "device_name": deviceName(),
	}, &out)
	if err != nil {
		if api.IsKind(err, api.KindUnauthorized) {
			return &api.Error{Kind: api.KindInvalid, Message: "invalid email or password"}
		}
		return err
	}
	if out.Token == "" {
		return &api.Error{Kind: api.KindServer, Message: "unexpected response from server"}
	}
	if err := s.keychain.Set(origin, out.Token); err != nil {
		// Do not leave a live session behind that we cannot store.
		_ = client.Do(ctx, "POST", "/v1/auth/logout", out.Token, nil, nil)
		return &api.Error{Kind: api.KindServer, Message: "could not save the session in the system keychain"}
	}
	if err := s.cfg.Update(func(c *config.Config) { c.Server, c.Email, c.InsecureTLS = origin, email, insecureTLS }); err != nil {
		return &api.Error{Kind: api.KindServer, Message: "could not save settings"}
	}
	s.mu.Lock()
	s.server, s.token, s.client = origin, out.Token, client
	s.mu.Unlock()
	return nil
}

// SignOut revokes the session on the server (best effort) and forgets it.
func (s *Service) SignOut(ctx context.Context) error {
	s.mu.Lock()
	client, token := s.client, s.token
	s.mu.Unlock()
	if client != nil && token != "" {
		_ = client.Do(ctx, "POST", "/v1/auth/logout", token, nil, nil)
	}
	s.forget()
	return nil
}

// forget deletes the stored token.
func (s *Service) forget() {
	s.mu.Lock()
	server := s.server
	s.token = ""
	s.mu.Unlock()
	if server != "" {
		_ = s.keychain.Delete(server)
	}
}

// call performs an authenticated request. On 401 the stored token is
// deleted so the frontend returns to the Connect screen.
func (s *Service) call(ctx context.Context, method, path string, in, out any) error {
	s.mu.Lock()
	client, token := s.client, s.token
	s.mu.Unlock()
	if client == nil || token == "" {
		return &api.Error{Kind: api.KindUnauthorized, Message: "signed out"}
	}
	err := client.Do(ctx, method, path, token, in, out)
	if api.IsKind(err, api.KindUnauthorized) {
		s.forget()
	}
	return err
}

func esc(id string) string { return url.PathEscape(id) }

// ---------------------------------------------------------------- projects

// ListProjects returns every project (archived included) for the board.
func (s *Service) ListProjects(ctx context.Context) ([]api.Project, error) {
	var out struct {
		Projects []api.Project `json:"projects"`
	}
	err := s.call(ctx, "GET", "/v1/projects", nil, &out)
	return out.Projects, err
}

// GetProject returns one project with all tasks and log entries.
func (s *Service) GetProject(ctx context.Context, id string) (api.Project, error) {
	var p api.Project
	err := s.call(ctx, "GET", "/v1/projects/"+esc(id), nil, &p)
	return p, err
}

// CreateProject creates a project with a name.
func (s *Service) CreateProject(ctx context.Context, name string) (api.Project, error) {
	var p api.Project
	err := s.call(ctx, "POST", "/v1/projects", map[string]any{"name": name}, &p)
	return p, err
}

// UpdateProject changes a project's name, description, tags or archived state.
func (s *Service) UpdateProject(ctx context.Context, id string, patch api.ProjectPatch) (api.Project, error) {
	var p api.Project
	err := s.call(ctx, "PATCH", "/v1/projects/"+esc(id), patch, &p)
	return p, err
}

// DeleteProject deletes a project.
func (s *Service) DeleteProject(ctx context.Context, id string) error {
	return s.call(ctx, "DELETE", "/v1/projects/"+esc(id), nil, nil)
}

// ReorderProjects sets the board order; ids must list every project.
func (s *Service) ReorderProjects(ctx context.Context, ids []string) error {
	return s.call(ctx, "PATCH", "/v1/projects/order", map[string][]string{"ids": ids}, nil)
}

// ListTags returns the user's tags with project counts.
func (s *Service) ListTags(ctx context.Context) ([]api.Tag, error) {
	var out struct {
		Tags []api.Tag `json:"tags"`
	}
	err := s.call(ctx, "GET", "/v1/tags", nil, &out)
	return out.Tags, err
}

// ---------------------------------------------------------------- log

// AddLogEntry adds a log entry to a project.
func (s *Service) AddLogEntry(ctx context.Context, projectID, body string) (api.LogEntry, error) {
	var e api.LogEntry
	err := s.call(ctx, "POST", "/v1/projects/"+esc(projectID)+"/log", map[string]string{"body": body}, &e)
	return e, err
}

// UpdateLogEntry edits a log entry.
func (s *Service) UpdateLogEntry(ctx context.Context, id, body string) (api.LogEntry, error) {
	var e api.LogEntry
	err := s.call(ctx, "PATCH", "/v1/log/"+esc(id), map[string]string{"body": body}, &e)
	return e, err
}

// DeleteLogEntry deletes a log entry.
func (s *Service) DeleteLogEntry(ctx context.Context, id string) error {
	return s.call(ctx, "DELETE", "/v1/log/"+esc(id), nil, nil)
}

// ---------------------------------------------------------------- tasks

// AddTask adds a task to a project.
func (s *Service) AddTask(ctx context.Context, projectID, title string) (api.Task, error) {
	var t api.Task
	err := s.call(ctx, "POST", "/v1/projects/"+esc(projectID)+"/tasks", map[string]string{"title": title}, &t)
	return t, err
}

// UpdateTask edits a task's title or done state.
func (s *Service) UpdateTask(ctx context.Context, id string, patch api.TaskPatch) (api.Task, error) {
	var t api.Task
	err := s.call(ctx, "PATCH", "/v1/tasks/"+esc(id), patch, &t)
	return t, err
}

// DeleteTask deletes a task.
func (s *Service) DeleteTask(ctx context.Context, id string) error {
	return s.call(ctx, "DELETE", "/v1/tasks/"+esc(id), nil, nil)
}

// ---------------------------------------------------------------- account

// ListSessions returns the signed-in devices.
func (s *Service) ListSessions(ctx context.Context) ([]api.Session, error) {
	var out struct {
		Sessions []api.Session `json:"sessions"`
	}
	err := s.call(ctx, "GET", "/v1/auth/sessions", nil, &out)
	return out.Sessions, err
}

// RevokeSession signs a device out. Revoking the current session signs this
// device out too.
func (s *Service) RevokeSession(ctx context.Context, id string, current bool) error {
	err := s.call(ctx, "DELETE", "/v1/auth/sessions/"+esc(id), nil, nil)
	if err == nil && current {
		s.forget()
	}
	return err
}

// ChangePassword changes the password; the server revokes all other sessions.
func (s *Service) ChangePassword(ctx context.Context, current, next string) error {
	if utf8.RuneCountInString(next) < 12 {
		return &api.Error{Kind: api.KindInvalid, Message: "the new password must be at least 12 characters"}
	}
	return s.call(ctx, "POST", "/v1/auth/password", map[string]string{
		"current_password": current, "new_password": next,
	}, nil)
}

// ---------------------------------------------------------------- local settings

// TagFilter returns the tag filter remembered on this device.
func (s *Service) TagFilter() []string {
	c, _ := s.cfg.Load()
	if c.TagFilter == nil {
		return []string{}
	}
	return c.TagFilter
}

// SetTagFilter remembers the tag filter on this device.
func (s *Service) SetTagFilter(tags []string) error {
	if len(tags) > 500 {
		return errors.New("too many tags")
	}
	return s.cfg.Update(func(c *config.Config) { c.TagFilter = tags })
}
