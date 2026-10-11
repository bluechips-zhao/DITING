package server

import (
	"fmt"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/norma/llm"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReportContainsAllFindingsAndOnlyOwnTask(t *testing.T) {
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	a, err := m.CreateTask("report-own", "own goal", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	b, err := m.CreateTask("other-task", "other goal", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer m.pg.Exec(`DELETE FROM findings WHERE task_id IN ($1,$2)`, a.ID, b.ID)
	defer m.pg.Exec(`DELETE FROM tasks WHERE id IN ($1,$2)`, a.ID, b.ID)
	if _, err = m.pg.Exec(`INSERT INTO findings(task_id,vulnclass,severity,summary,evidence,status) SELECT $1,'test','medium','own-'||n,'proof','pending' FROM generate_series(1,1007) n`, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = m.pg.Exec(`INSERT INTO findings(task_id,vulnclass,severity,summary,evidence) VALUES($1,'test','high','excluded-foreign-marker','proof')`, b.ID); err != nil {
		t.Fatal(err)
	}
	s := newStoppedTestServer(m, t.TempDir(), t.TempDir(), t.TempDir())
	r := httptest.NewRequest("GET", "/api/report?task="+a.ID, nil)
	w := httptest.NewRecorder()
	s.getReport(w, r)
	out := w.Body.String()
	if w.Code != 200 || !strings.Contains(out, "**1007**") || !strings.Contains(out, "own-1007") || strings.Contains(out, "excluded-foreign-marker") {
		t.Fatalf("report scope/count failed code=%d", w.Code)
	}
}

func TestActualProviderAndToolCallsSharePersistentBudget(t *testing.T) {
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	task, err := m.pg.CreateTask("budget provider", "mock", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer m.pg.DeleteTask(task.ID)
	if err = m.pg.SetTaskBudget(t.Context(), db.TaskBudget{TaskID: task.ID, TokenLimit: 30000, ToolLimit: 2, ReserveTokens: 1000, ReserveTools: 1}); err != nil {
		t.Fatal(err)
	}
	s := newStoppedTestServer(m, t.TempDir(), t.TempDir(), t.TempDir())
	provider := observedTaskProvider{engine: s.engine, taskID: fmt.Sprint(task.ID), base: &scriptedLLMProvider{events: []llm.StreamEvent{{Usage: llm.Usage{InputTokens: 20, OutputTokens: 10}}}}}
	for range provider.Stream(t.Context(), llm.CompletionRequest{MaxTokens: 10}) {
	}
	budget, err := m.pg.TaskBudget(t.Context(), task.ID)
	if err != nil || budget.TokensUsed != 30 || budget.TokensReserved != 0 {
		t.Fatalf("actual usage not reconciled %+v %v", budget, err)
	}
	hooks := taskBudgetHooks{s: s, id: task.ID}
	if block, _, _ := hooks.PreToolUse(t.Context(), "Bash", nil); block {
		t.Fatal("first admitted tool blocked")
	}
	if block, _, _ := hooks.PreToolUse(t.Context(), "Bash", nil); !block {
		t.Fatal("normal tools exceeded threshold")
	}
	if block, _, _ := hooks.PreToolUse(t.Context(), "record_fact", nil); block {
		t.Fatal("settlement write denied")
	}
	if block, _, _ := hooks.PreToolUse(t.Context(), "record_fact", nil); !block {
		t.Fatal("hard tool budget exceeded")
	}
}
