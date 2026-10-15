package server

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"testing"
	"time"
)

// Live server fixtures cannot inherit tasks or providers from metadata tests.
func newIsolatedTestManager(t *testing.T) *Manager {
	t.Helper()
	u, err := url.Parse(os.Getenv("ARTEX_PG_DSN"))
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || (u.Hostname() != "localhost" && u.Hostname() != "127.0.0.1") {
		t.Skip("live fixture requires explicit local ARTEX_PG_DSN")
	}
	u.Path = "/postgres"
	admin, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("diting_live_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec(`CREATE DATABASE "` + name + `"`); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	u.Path = "/" + name
	t.Setenv("ARTEX_PG_DSN", u.String())
	var m *Manager
	t.Cleanup(func() {
		if m != nil {
			m.Close()
		}
		if _, err := admin.Exec(`DROP DATABASE "` + name + `"`); err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	m, err = NewManager(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	// This fixture exercises task lifecycle, not MCP process discovery.
	if _, err = m.pg.Exec(`UPDATE mcp_servers SET enabled=false`); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestCanceledEngineRunDoesNotStartTask(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e := &Engine{}
	e.Run(ctx, &Task{ID: "cancelled"})
	if _, started := e.started.Load("cancelled"); started {
		t.Fatal("cancelled task registered")
	}
}
