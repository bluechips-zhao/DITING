package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"testing"
)

func TestDashboardSummaryPreservesAllTasksAndLegacyTokens(t *testing.T) {
	profile := int64(9)
	tasks := make([]TaskDTO, 9)
	for i := range tasks {
		tasks[i] = TaskDTO{
			ID: strconv.Itoa(i + 1), Status: "paused", CreatedAt: "2025-01-02T12:00:00Z",
			Tokens: TokenTotalDTO{InputTokens: i + 1, OutputTokens: 2, CacheReadTokens: 3, CacheWriteTokens: 4},
		}
	}
	tasks[0].LLMProfileID = &profile
	tasks[1].LLMProfileID = &profile
	tasks[1].CreatedAt = "2026-10-16T01:00:00+08:00"
	tasks[2].LastActivity = 100
	tasks[4].LastActivity = 100
	tasks[8].Status = "done"
	before, _ := json.Marshal(tasks)
	got := summarizeDashboardTasks(tasks)
	if got.Total != 9 || got.StatusCounts["paused"] != 8 || got.StatusCounts["done"] != 1 {
		t.Fatalf("counts do not cover all tasks: %+v", got)
	}
	ids := []string{}
	for _, task := range got.Recent {
		ids = append(ids, task.ID)
	}
	if !reflect.DeepEqual(ids, []string{"3", "5", "1", "2", "4"}) {
		t.Fatalf("recent tasks lost stable tie order: %v", ids)
	}
	var count int
	var tokens TokenTotalDTO
	for _, group := range got.TokenGroups {
		count += group.Tasks
		tokens.InputTokens += group.Tokens.InputTokens
		tokens.OutputTokens += group.Tokens.OutputTokens
		tokens.CacheReadTokens += group.Tokens.CacheReadTokens
		tokens.CacheWriteTokens += group.Tokens.CacheWriteTokens
	}
	if count != 9 || tokens != (TokenTotalDTO{45, 18, 27, 36}) {
		t.Fatalf("tokens truncated to recent table: count=%d tokens=%+v", count, tokens)
	}
	if len(got.TokenGroups) != 3 || got.TokenGroups[1].Date != "2026-10-16" ||
		got.TokenGroups[2].ProfileID != nil || got.TokenGroups[2].Tasks != 7 {
		t.Fatalf("legacy profile/date grouping changed: %+v", got.TokenGroups)
	}
	after, _ := json.Marshal(tasks)
	if string(before) != string(after) {
		t.Fatal("summary mutated source task order or fields")
	}
	empty, _ := json.Marshal(summarizeDashboardTasks(nil))
	if string(empty) != `{"total":0,"status_counts":{},"recent":[],"token_groups":[]}` {
		t.Fatalf("empty summary shape: %s", empty)
	}
}

func TestDashboardTasksAuthenticatedAndMatchesFullSnapshot(t *testing.T) {
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Skipf("postgres unavailable (%v) - skipping", err)
	}
	defer m.Close()
	for i := 0; i < 8; i++ {
		task, err := m.CreateTask("dashboard synthetic fixture", "goal", nil, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := strconv.ParseInt(task.ID, 10, 64)
		t.Cleanup(func() { _ = m.pg.DeleteTask(id) })
	}
	s := newStoppedTestServer(m, t.TempDir(), t.TempDir(), t.TempDir())
	handler := s.Handler()
	request := func(path, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	if rec := request("/api/dashboard/tasks", ""); rec.Code != http.StatusUnauthorized {
		t.Fatalf("dashboard summary unauthenticated status=%d", rec.Code)
	}
	token, err := s.issueToken()
	if err != nil {
		t.Fatal(err)
	}
	full := request("/api/tasks", token)
	summary := request("/api/dashboard/tasks", token)
	if full.Code != 200 || summary.Code != 200 {
		t.Fatalf("full=%d summary=%d", full.Code, summary.Code)
	}
	var source struct {
		Tasks []TaskDTO `json:"tasks"`
	}
	var got dashboardTaskSummary
	if err := json.Unmarshal(full.Body.Bytes(), &source); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(summary.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, summarizeDashboardTasks(source.Tasks)) {
		t.Fatal("compact endpoint differs from full task snapshot")
	}
	if got.Total != 8 || len(got.Recent) != 5 {
		t.Fatalf("total=%d recent=%d", got.Total, len(got.Recent))
	}
	if summary.Body.Len() >= full.Body.Len() {
		t.Fatal("summary still transfers every full task")
	}
}
