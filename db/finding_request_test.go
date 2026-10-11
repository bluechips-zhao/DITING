package db

import (
	"fmt"
	"testing"
	"time"
)

func TestFindingRequestIdempotencyAndCandidate(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	task, err := d.CreateTask("idempotent finding", "local test", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.DeleteTask(task.ID) })
	es := d.Exploration(task.ExplorationID)
	in := RecordFindingInput{TaskID: task.ID, Summary: "missing proof", Severity: "high", IdempotencyKey: fmt.Sprintf("request-%d", time.Now().UnixNano())}
	one, err := es.RecordFinding(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = d.DeleteFinding(one.FindingID) })
	n, _ := es.GetNode(one.NodeID)
	if n.State != "candidate" {
		t.Fatal("unproven finding labeled confirmed")
	}
	two, err := es.RecordFinding(t.Context(), in)
	if err != nil || !two.Reused || two.FindingID != one.FindingID {
		t.Fatalf("retry: %+v %v", two, err)
	}
	in.Summary = "different finding"
	if _, err = es.RecordFinding(t.Context(), in); err == nil {
		t.Fatal("reused key accepted different content")
	}
	in.IdempotencyKey = ""
	in.Severity = "dangerous"
	if _, err = es.RecordFinding(t.Context(), in); err == nil {
		t.Fatal("invalid severity accepted")
	}
	if _, _, err = es.ProveGoal(t.Context(), one.NodeID, one.NodeID, "candidate"); err == nil {
		t.Fatal("invalid goal accepted")
	}
}
