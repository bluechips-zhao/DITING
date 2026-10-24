package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Autumn-27/artex/config"
)

func fixtureDatabase() config.Database {
	return config.Database{Host: "127.0.0.1", Port: 5432, User: "fixture", Password: "fixture-password", DBName: "fixture", SSLMode: "disable"}
}

func TestSavePreservesSettingsAndRecoverableConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	original := []byte(`{"skill_dir":"keep","custom":{"keep":true},"database":{"host":"old"}}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err := SaveDatabase(path, fixtureDatabase()); err != nil {
		t.Fatal(err)
	}
	actual, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(actual, &fields); err != nil {
		t.Fatal(err)
	}
	if string(fields["skill_dir"]) != `"keep"` || !strings.Contains(string(fields["custom"]), `"keep": true`) {
		t.Fatal("unrelated config lost")
	}
	backups, err := filepath.Glob(path + ".before-setup-*.backup")
	if err != nil || len(backups) != 1 {
		t.Fatal("backup missing")
	}
	prior, err := os.ReadFile(backups[0])
	if err != nil || string(prior) != string(original) {
		t.Fatal("backup differs from original")
	}
	broken := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(broken, []byte("broken"), 0600)
	if err = SaveDatabase(broken, fixtureDatabase()); err == nil {
		t.Fatal("invalid config overwritten")
	}
	prior, _ = os.ReadFile(broken)
	if string(prior) != "broken" {
		t.Fatal("broken config changed")
	}
	fresh := filepath.Join(t.TempDir(), "new", "config.json")
	if err = SaveDatabase(fresh, fixtureDatabase()); err != nil {
		t.Fatal(err)
	}
}

func wizardRequest(w *Wizard, method, path, body, host, remote, origin, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Host = host
	r.RemoteAddr = remote
	if origin != "" {
		r.Header.Set("Origin", origin)
	}
	if token != "" {
		r.Header.Set("X-Diting-Setup", token)
	}
	rec := httptest.NewRecorder()
	w.Handler().ServeHTTP(rec, r)
	return rec
}

func TestWizardRejectsRemoteOriginsAndMissingTokens(t *testing.T) {
	w, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct{ host, remote, origin, token string }{
		{"localhost:8787", "192.0.2.1:1234", "", w.token},
		{"rebind.invalid:8787", "127.0.0.1:1234", "", w.token},
		{"localhost:8787", "127.0.0.1:1234", "http://other.invalid", w.token},
		{"localhost:8787", "127.0.0.1:1234", "", ""},
		{"localhost:8787", "127.0.0.1:1234", "", "wrong"},
	} {
		if rec := wizardRequest(w, "POST", "/api/bootstrap/save", "{}", c.host, c.remote, c.origin, c.token); rec.Code != 403 {
			t.Fatalf("request accepted: status %d", rec.Code)
		}
	}
	rec := wizardRequest(w, "GET", "/", "", "localhost:8787", "127.0.0.1:1234", "", "")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), w.token) || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("local UI unavailable")
	}
}

func TestWizardChecksBeforeSaveAndHonorsEnvironment(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	checks := 0
	w, err := New(Options{ConfigPath: path, Check: func(context.Context, string) error { checks++; return errors.New("fixture unavailable") }})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(fixtureDatabase())
	trailing := wizardRequest(w, "POST", "/api/bootstrap/save", string(body)+"{}", "127.0.0.1:8787", "127.0.0.1:1234", "", w.token)
	if trailing.Code != 400 {
		t.Fatal("trailing JSON accepted")
	}
	call := func(path string) *httptest.ResponseRecorder {
		return wizardRequest(w, "POST", path, string(body), "127.0.0.1:8787", "127.0.0.1:1234", "http://127.0.0.1:8787", w.token)
	}
	if rec := call("/api/bootstrap/save"); rec.Code != 422 {
		t.Fatal("bad connection accepted")
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("failed check created config")
	}
	w.opts.Check = func(context.Context, string) error { checks++; return nil }
	if rec := call("/api/bootstrap/check"); rec.Code != 200 {
		t.Fatal("check failed")
	}
	if _, err = os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("test-only request wrote config")
	}
	if rec := call("/api/bootstrap/save"); rec.Code != 200 {
		t.Fatal("save failed")
	}
	select {
	case <-w.done:
	default:
		t.Fatal("save did not release startup")
	}
	if rec := call("/api/bootstrap/save"); rec.Code != 409 {
		t.Fatal("duplicate save accepted")
	}
	if checks != 3 {
		t.Fatalf("checks=%d", checks)
	}
	envPath := filepath.Join(t.TempDir(), "config.json")
	env, err := New(Options{ConfigPath: envPath, EnvironmentDSN: true, ExistingDSN: "fixture", Check: func(context.Context, string) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	if rec := wizardRequest(env, "POST", "/api/bootstrap/save", string(body), "localhost:8787", "127.0.0.1:1234", "", env.token); rec.Code != 409 {
		t.Fatal("environment config overwritten")
	}
	if rec := wizardRequest(env, "POST", "/api/bootstrap/retry", "{}", "localhost:8787", "127.0.0.1:1234", "", env.token); rec.Code != 200 {
		t.Fatal("environment retry failed")
	}
	if _, err = os.Stat(envPath); !os.IsNotExist(err) {
		t.Fatal("environment retry created unused file")
	}
}

func TestEnsureDatabaseUsesLoopbackAndCancelsCleanly(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := strings.Split(listener.Addr().String(), ":")[1]
	listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- EnsureDatabase(ctx, ":"+port, Options{ConfigPath: filepath.Join(t.TempDir(), "config.json")})
	}()
	client := http.Client{Timeout: time.Second}
	address := "http://127.0.0.1:" + port + "/"
	deadline := time.Now().Add(5 * time.Second)
	seen := false
	for time.Now().Before(deadline) {
		response, err := client.Get(address)
		if err == nil {
			response.Body.Close()
			seen = response.StatusCode == 200
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !seen {
		t.Fatal("wizard did not listen")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("wizard did not stop")
	}
	probe, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		t.Fatal("wizard retained port")
	}
	probe.Close()
}

func TestDatabaseCheckRedactsConnectionFailures(t *testing.T) {
	err := DatabaseCheck(context.Background(), "postgres://fixture:DO_NOT_DISCLOSE@127.0.0.1:1/missing?sslmode=disable")
	if err == nil || strings.Contains(err.Error(), "DO_NOT_DISCLOSE") {
		t.Fatal("connection failure exposed password")
	}
}

func TestEnsureDatabaseAutomaticallyRecoversConfiguredDatabase(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	var calls atomic.Int32
	path := filepath.Join(t.TempDir(), "config.json")
	err := EnsureDatabase(ctx, "127.0.0.1:0", Options{
		ConfigPath: path, ExistingDSN: "fixture", EnvironmentDSN: true,
		Check: func(_ context.Context, dsn string) error {
			if dsn != "fixture" {
				t.Errorf("unexpected database selected")
			}
			if calls.Add(1) == 1 {
				return errors.New("temporarily unavailable")
			}
			return nil
		},
	})
	if err != nil || calls.Load() != 2 {
		t.Fatalf("recovery err=%v calls=%d", err, calls.Load())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("automatic retry must not create or replace configuration")
	}
}

func TestEnsureDatabaseCancelsAutomaticDatabaseRetry(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	var calls atomic.Int32
	err := EnsureDatabase(ctx, "127.0.0.1:0", Options{
		ExistingDSN: "fixture",
		Check: func(ctx context.Context, _ string) error {
			if calls.Add(1) > 1 {
				cancel()
				<-ctx.Done()
				return ctx.Err()
			}
			return errors.New("temporarily unavailable")
		},
	})
	if !errors.Is(err, context.Canceled) || calls.Load() != 2 {
		t.Fatalf("cancel err=%v calls=%d", err, calls.Load())
	}
}
