package server

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExecutionStatsHTTPRequiresAuthAndKeepsTaskScope(t *testing.T) {
	m := newIsolatedTestManager(t)
	task, err := m.pg.CreateTask("own stats", "local", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	other, err := m.pg.CreateTask("other stats", "local", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int64{task.ID, other.ID} {
		call, err := m.pg.StartModelCall(t.Context(), id, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err = m.pg.FinishModelCall(t.Context(), call, 10, 20, 30, "completed", true); err != nil {
			t.Fatal(err)
		}
	}
	s := &Server{m: m, jwtKey: []byte("stats-fixture-key")}
	h := s.Handler()
	token, err := s.issueToken()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		path string
		auth bool
		code int
	}{
		{fmt.Sprintf("/api/tasks/%d/execution-stats", task.ID), false, 401},
		{"/api/tasks/invalid/execution-stats", true, 400},
		{"/api/tasks/9223372036854775807/execution-stats", true, 404},
		{fmt.Sprintf("/api/tasks/%d/execution-stats", task.ID), true, 200},
	} {
		r := httptest.NewRequest(http.MethodGet, tc.path, nil)
		if tc.auth {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.code {
			t.Fatalf("path=%s status=%d body=%s", tc.path, w.Code, w.Body)
		}
		if w.Code == 200 && (!strings.Contains(w.Body.String(), `"model_calls":1`) || strings.Contains(w.Body.String(), `"model_calls":2`)) {
			t.Fatalf("foreign stats leaked: %s", w.Body)
		}
	}
}
