package server

import (
	"database/sql"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/db"
)

func TestHealthRejectsUnavailableDatabaseWithoutLeakingConnection(t *testing.T) {
	pool, err := sql.Open("pgx", "postgres://fixture:private-password@127.0.0.1:1/fixture")
	if err != nil {
		t.Fatal(err)
	}
	pool.Close()
	for _, server := range []*Server{{}, {m: &Manager{pg: &db.DB{DB: pool}}}} {
		w := httptest.NewRecorder()
		server.health(w, httptest.NewRequest("GET", "/api/health", nil))
		if w.Code != 503 || !strings.Contains(w.Body.String(), `"ok":false`) {
			t.Fatalf("unavailable database reported healthy: %d %s", w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "private-password") {
			t.Fatal("health response leaked connection")
		}
	}
}
