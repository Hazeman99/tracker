package tracker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"tracker/desktop/internal/api"
	"tracker/desktop/internal/config"
	"tracker/desktop/internal/secrets"
)

type memKeychain struct {
	mu sync.Mutex
	m  map[string]string
}

func (k *memKeychain) Get(s string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	v, ok := k.m[s]
	if !ok {
		return "", secrets.ErrNotFound
	}
	return v, nil
}
func (k *memKeychain) Set(s, t string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.m[s] = t
	return nil
}
func (k *memKeychain) Delete(s string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.m, s)
	return nil
}

const fakeToken = "tok_abcdefghijklmnopqrstuvwxyz0123456789ABCDE"

func setup(t *testing.T, handler http.HandlerFunc) (*Service, *memKeychain, string, string) {
	t.Helper()
	ts := httptest.NewTLSServer(handler)
	t.Cleanup(ts.Close)
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	kc := &memKeychain{m: map[string]string{}}
	s := New(config.NewStoreAt(cfgPath), kc)
	s.newAPI = func(base string, _ bool) *api.Client { return api.NewWithHTTPClient(base, ts.Client()) }
	return s, kc, ts.URL, cfgPath
}

func TestConnectStoresTokenOnlyInKeychain(t *testing.T) {
	var sawAuth string
	s, kc, url, cfgPath := setup(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/auth/login":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["email"] != "a@example.com" || body["password"] != "pw" || body["device_name"] == "" {
				w.WriteHeader(401)
				return
			}
			_, _ = w.Write([]byte(`{"token":"` + fakeToken + `"}`))
		case "/v1/projects":
			sawAuth = r.Header.Get("Authorization")
			_, _ = w.Write([]byte(`{"projects":[]}`))
		}
	})
	if err := s.Connect(context.Background(), url, "a@example.com", "pw", false); err != nil {
		t.Fatal(err)
	}
	if kc.m[url] != fakeToken {
		t.Fatal("token not in keychain")
	}
	b, _ := os.ReadFile(cfgPath)
	if strings.Contains(string(b), fakeToken) {
		t.Fatal("token written to config file")
	}
	fi, _ := os.Stat(cfgPath)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("config perms %v", fi.Mode().Perm())
	}
	st := s.Status()
	raw, _ := json.Marshal(st)
	if !st.Connected || strings.Contains(string(raw), fakeToken) {
		t.Fatalf("status %s", raw)
	}
	if _, err := s.ListProjects(context.Background()); err != nil {
		t.Fatal(err)
	}
	if sawAuth != "Bearer "+fakeToken {
		t.Fatalf("auth header %q", sawAuth)
	}
}

func TestUnauthorizedClearsToken(t *testing.T) {
	s, kc, url, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/login" {
			_, _ = w.Write([]byte(`{"token":"` + fakeToken + `"}`))
			return
		}
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	})
	if err := s.Connect(context.Background(), url, "a@example.com", "pw", false); err != nil {
		t.Fatal(err)
	}
	_, err := s.ListProjects(context.Background())
	if !api.IsKind(err, api.KindUnauthorized) {
		t.Fatalf("err %v", err)
	}
	if _, ok := kc.m[url]; ok || s.Status().Connected {
		t.Fatal("token not cleared after 401")
	}
}

func TestRefusesPlainHTTP(t *testing.T) {
	s, _, _, _ := setup(t, func(http.ResponseWriter, *http.Request) {})
	err := s.Connect(context.Background(), "http://tracker.example.com", "a@example.com", "pw", false)
	if !api.IsKind(err, api.KindInvalid) {
		t.Fatalf("plain http accepted: %v", err)
	}
}

func TestUntrustedCertificateRejected(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"token":"x"}`))
	}))
	defer ts.Close()
	s := New(config.NewStoreAt(filepath.Join(t.TempDir(), "c.json")), &memKeychain{m: map[string]string{}})
	// Default client: system roots only, so the test certificate is refused.
	err := s.Connect(context.Background(), ts.URL, "a@example.com", "pw", false)
	if !api.IsKind(err, api.KindOffline) || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("self-signed certificate accepted or wrong error: %v", err)
	}
}

func TestInsecureTLSOptInAcceptsSelfSigned(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/login" {
			_, _ = w.Write([]byte(`{"token":"` + fakeToken + `"}`))
			return
		}
		_, _ = w.Write([]byte(`{"projects":[]}`))
	}))
	defer ts.Close()
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	kc := &memKeychain{m: map[string]string{}}
	s := New(config.NewStoreAt(cfgPath), kc)
	// Real client (api.New), with verification disabled by the user.
	if err := s.Connect(context.Background(), ts.URL, "a@example.com", "pw", true); err != nil {
		t.Fatalf("connect with insecure TLS: %v", err)
	}
	if c, _ := config.NewStoreAt(cfgPath).Load(); !c.InsecureTLS {
		t.Fatal("insecure_tls not persisted")
	}
	if st := s.Status(); !st.InsecureTLS {
		t.Fatal("status does not report insecure_tls")
	}
	// A restored service (restart) must honour the saved flag.
	s2 := New(config.NewStoreAt(cfgPath), kc)
	if !s2.Status().Connected {
		t.Fatal("session not restored")
	}
	if _, err := s2.ListProjects(context.Background()); err != nil {
		t.Fatalf("restored service ignores insecure_tls: %v", err)
	}
}

func TestRedirectsNotFollowed(t *testing.T) {
	s, _, url, _ := setup(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.example/steal", http.StatusFound)
	})
	err := s.Connect(context.Background(), url, "a@example.com", "pw", false)
	if err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("redirect: %v", err)
	}
}

func TestOnboardingIsRememberedPerDevice(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	s := New(config.NewStoreAt(cfgPath), &memKeychain{m: map[string]string{}})
	if s.Status().Onboarded {
		t.Fatal("fresh install already onboarded")
	}
	if err := s.CompleteOnboarding(); err != nil {
		t.Fatal(err)
	}
	// A new process reading the same config file sees the flag.
	s2 := New(config.NewStoreAt(cfgPath), &memKeychain{m: map[string]string{}})
	if !s2.Status().Onboarded {
		t.Fatal("onboarding flag not persisted")
	}
}
