package server

import (
	"github.com/Autumn-27/artex/db"
	"testing"
)

func TestServerStartupRecoversPreviousProcessApprovals(t *testing.T) {
	m := newIsolatedTestManager(t)
	create := func(call string) int64 {
		t.Helper()
		id, err := m.pg.CreateInterceptPending(0, 0, "startup-recovery", "test", "Write", []byte(`{}`), "review", &db.InterceptAudit{
			RunID: "old-process", ToolUseID: call, Correlation: "exact", InitialAction: "ask", ExecutionStatus: "not_started",
		})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	pending, allowed := create("pending"), create("allowed")
	if _, err := m.pg.ResolveIntercept(allowed, "allowed", "allow", "manual approval"); err != nil {
		t.Fatal(err)
	}
	newStoppedTestServer(m, t.TempDir(), t.TempDir(), t.TempDir())
	for _, tc := range []struct {
		id                  int64
		decision, execution string
	}{
		{pending, "denied", "not_executed"}, {allowed, "allowed", "unknown"},
	} {
		detail, err := m.pg.GetInterceptDetail(tc.id)
		if err != nil || detail == nil || detail.Status != tc.decision || detail.Audit == nil || detail.Audit.ExecutionStatus != tc.execution {
			t.Fatalf("startup approval recovery: detail=%+v err=%v", detail, err)
		}
	}
}
