package db

import (
	"strings"
	"testing"
)

func TestInterruptedApprovalsRequireNewDecisionAndPreserveUnknownExecution(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	create := func(audit *InterceptAudit) int64 {
		t.Helper()
		id, err := d.CreateInterceptPending(0, 0, "recovery-test", "test", "Write", []byte(`{}`), "original reason", audit)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = d.Exec(`DELETE FROM intercept_pending WHERE id=$1`, id) })
		return id
	}
	newAudit := func(call string) *InterceptAudit {
		return &InterceptAudit{RunID: "run", ToolUseID: call, Correlation: "exact", InitialAction: "ask", ExecutionStatus: "not_started", UserMessage: "preserve original context"}
	}
	pending := create(newAudit("pending"))
	legacy := create(nil)
	allowed := create(newAudit("allowed"))
	completed := create(newAudit("completed"))
	for _, id := range []int64{allowed, completed} {
		if _, err := d.ResolveIntercept(id, "allowed", "allow", "manual approval"); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.CompleteIntercept(completed, "run", "completed", "succeeded", "original output", false); err != nil {
		t.Fatal(err)
	}
	n, err := d.RecoverInterruptedIntercepts(t.Context())
	if err != nil || n < 3 {
		t.Fatalf("recovery count=%d err=%v", n, err)
	}
	for _, id := range []int64{pending, legacy} {
		detail, err := d.GetInterceptDetail(id)
		if err != nil || detail.Status != "denied" || !strings.Contains(detail.Reason, "重新审批") {
			t.Fatalf("orphan=%+v err=%v", detail, err)
		}
		if ok, err := d.ResolveIntercept(id, "allowed", "allow", "late decision"); err != nil || ok {
			t.Fatalf("stale approval accepted: %v %v", ok, err)
		}
		if id == pending && (detail.Audit.EffectiveAction != "deny" || detail.Audit.ExecutionStatus != "not_executed" || detail.Audit.UserMessage != "preserve original context") {
			t.Fatalf("pending audit=%+v", detail.Audit)
		}
		if id == legacy && detail.Audit != nil {
			t.Fatal("invented legacy audit")
		}
	}
	detail, err := d.GetInterceptDetail(allowed)
	if err != nil || detail.Status != "allowed" || detail.Audit.EffectiveAction != "allow" || detail.Audit.ExecutionStatus != "unknown" || detail.Audit.DecisionReason != "manual approval" || !strings.Contains(detail.Audit.Output, "可能已经执行") {
		t.Fatalf("unknown execution=%+v err=%v", detail, err)
	}
	if err = d.CompleteIntercept(allowed, "run", "allowed", "succeeded", "late result", false); err != nil {
		t.Fatal(err)
	}
	n, err = d.RecoverInterruptedIntercepts(t.Context())
	if err != nil || n != 0 {
		t.Fatalf("recovery not idempotent: %d %v", n, err)
	}
	detail, err = d.GetInterceptDetail(completed)
	if err != nil || detail.Audit.ExecutionStatus != "succeeded" || detail.Audit.Output != "original output" {
		t.Fatalf("completed result rewritten: %+v %v", detail, err)
	}
}
