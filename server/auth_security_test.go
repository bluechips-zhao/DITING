package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

func TestDamagedJWTKeyIsNotSilentlyReplaced(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, jwtKeyFilename)
	if err := os.WriteFile(path, []byte("damaged"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadOrCreateJWTKey(dir, t.TempDir()); err == nil {
		t.Fatal("damaged key accepted")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "damaged" {
		t.Fatal("original key overwritten")
	}
}

func TestJWTRequiresExpectedAlgorithmSubjectAndExpiry(t *testing.T) {
	key := []byte(strings.Repeat("k", 32))
	for _, tc := range []struct {
		method jwt.SigningMethod
		claims jwt.RegisteredClaims
	}{
		{jwt.SigningMethodHS384, jwt.RegisteredClaims{Subject: defaultUsername, ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}},
		{jwt.SigningMethodHS256, jwt.RegisteredClaims{Subject: "other", ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}},
		{jwt.SigningMethodHS256, jwt.RegisteredClaims{Subject: defaultUsername}},
	} {
		token, err := jwt.NewWithClaims(tc.method, tc.claims).SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		if verifyJWT(token, key) {
			t.Fatal("accepted invalid token policy")
		}
	}
}

func TestLoginLimitBoundsAttemptsAndIgnoresForgedForwarding(t *testing.T) {
	limiter := loginLimiter{}
	now := time.Now()
	r := httptest.NewRequest("POST", "/api/auth/login", nil)
	r.RemoteAddr = "127.0.0.1:1000"
	for i := 0; i < loginMaxAttempts; i++ {
		r.Header.Set("X-Forwarded-For", strings.Repeat("x", i+1))
		if allowed, _ := limiter.allow(r, now); !allowed {
			t.Fatal("limited too soon")
		}
	}
	r.RemoteAddr = "127.0.0.1:2000"
	if allowed, retry := limiter.allow(r, now); allowed || retry <= 0 {
		t.Fatal("port change bypassed login limit")
	}
	if allowed, _ := limiter.allow(r, now.Add(loginWindow)); !allowed {
		t.Fatal("window did not expire")
	}
}

func TestPasswordChangeRevokesOldSessionAcrossRestart(t *testing.T) {
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	defer m.Close()
	previous, existed, err := m.pg.GetSetting(authPassKey)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if existed {
			_ = m.pg.SetSetting(authPassKey, previous)
		} else {
			_, _ = m.pg.Exec(`DELETE FROM settings WHERE key=$1`, authPassKey)
		}
	}()
	hash, _ := bcrypt.GenerateFromPassword([]byte("previous-pass"), bcrypt.MinCost)
	if err := m.pg.SetSetting(authPassKey, string(hash)); err != nil {
		t.Fatal(err)
	}
	s := &Server{m: m, jwtKey: []byte(strings.Repeat("s", 32))}
	old, err := s.issueToken()
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "/api/auth/change-password", strings.NewReader(`{"old_password":"previous-pass","new_password":"changed-pass"}`))
	request.Header.Set("Authorization", "Bearer "+old)
	w := httptest.NewRecorder()
	s.authChangePassword(w, request)
	if w.Code != 200 {
		t.Fatalf("change: %d %s", w.Code, w.Body.String())
	}
	protected := func(server *Server, token string) int {
		r := httptest.NewRequest("GET", "/api/private", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		server.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })).ServeHTTP(w, r)
		return w.Code
	}
	if code := protected(s, old); code != 401 {
		t.Fatalf("old session: %d", code)
	}
	login := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"diting","password":"changed-pass"}`))
	w = httptest.NewRecorder()
	s.authLogin(w, login)
	var result struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || w.Code != 200 {
		t.Fatalf("new login: %s", w.Body.String())
	}
	restarted := &Server{m: m, jwtKey: append([]byte(nil), s.jwtKey...)}
	if protected(restarted, old) != 401 || protected(restarted, result.Token) != 204 {
		t.Fatal("restart restored invalidated session or rejected new session")
	}
}

func TestURLTokenIsNeverAccepted(t *testing.T) {
	s := &Server{jwtKey: []byte(strings.Repeat("s", 32))}
	token, _ := signJWT(s.jwtKey)
	r := httptest.NewRequest("GET", "/api/update/status?token="+token, nil)
	w := httptest.NewRecorder()
	s.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })).ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("URL credential accepted: %d", w.Code)
	}
}

func TestOpenStreamStopsAfterPasswordRevocation(t *testing.T) {
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Skipf("postgres unavailable: %v", err)
	}
	defer m.Close()
	previous, existed, err := m.pg.GetSetting(authPassKey)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if existed {
			_ = m.pg.SetSetting(authPassKey, previous)
		} else {
			_, _ = m.pg.Exec(`DELETE FROM settings WHERE key=$1`, authPassKey)
		}
	}()
	if err := m.pg.SetSetting(authPassKey, "test-password-state"); err != nil {
		t.Fatal(err)
	}
	s := &Server{m: m, jwtKey: []byte(strings.Repeat("s", 32))}
	token, err := s.issueToken()
	if err != nil {
		t.Fatal(err)
	}
	stopped := make(chan struct{})
	host := httptest.NewServer(s.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		<-r.Context().Done()
		close(stopped)
	})))
	defer host.Close()
	r, _ := http.NewRequest("GET", host.URL+"/api/test/stream", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("Accept", "text/event-stream")
	response, err := host.Client().Do(r)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatalf("stream status: %d", response.StatusCode)
	}
	if err := m.pg.SetSetting(authPassKey, "changed-password-state"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-stopped:
	case <-time.After(8 * time.Second):
		t.Fatal("revoked stream remained open")
	}
}
