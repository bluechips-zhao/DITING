package db

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestProveGoalAtomicAndUnbounded(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	exp, err := d.CreateExploration("proof test", "goals")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = d.Exec(`DELETE FROM explorations WHERE id=$1`, exp) })
	es := d.Exploration(exp)
	goal, err := es.AddGoal(map[string]any{"text": "verify"}, "human")
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := es.AddNode(KindFact, map[string]any{"summary": "observed"}, 0, "confirmed", "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO exploration_nodes(exploration_id,kind,payload,state) SELECT $1,'goal','{}','met' FROM generate_series(1,1001)`, exp); err != nil {
		t.Fatal(err)
	}
	last, err := es.AddGoal(map[string]any{"text": "still open"}, "human")
	if err != nil {
		t.Fatal(err)
	}
	if all, total, err := es.ProveGoal(t.Context(), goal, evidence, "local observation"); err != nil || all || total != 1003 {
		t.Fatalf("all=%v total=%d err=%v", all, total, err)
	}
	n, err := es.GetNode(goal)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Proof GoalProof `json:"proof"`
	}
	_ = json.Unmarshal(n.Payload, &payload)
	if payload.Proof.Reason != "local observation" || n.State != "met" {
		t.Fatalf("proof/state not persisted: %+v", n)
	}
	if _, _, err := es.ProveGoal(t.Context(), last, 999999999999, "missing evidence"); err == nil {
		t.Fatal("invalid evidence accepted")
	}
	n, _ = es.GetNode(last)
	if n.State != "open" {
		t.Fatal("failure changed goal state")
	}
	if all, total, err := es.ProveGoal(t.Context(), last, evidence, "satisfies last goal"); err != nil || !all || total != 1003 {
		t.Fatalf("all=%v total=%d err=%v", all, total, err)
	}
}

func TestProveGoalRollsBackLateFailure(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	exp, err := d.CreateExploration("rollback", "proof")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Exec(`DELETE FROM explorations WHERE id=$1`, exp)
	es := d.Exploration(exp)
	goal, err := es.AddGoal(map[string]any{"text": "check"}, "human")
	if err != nil {
		t.Fatal(err)
	}
	ev, err := es.AddNode(KindFact, map[string]any{"summary": "observed"}, 0, "confirmed", "test", nil)
	if err != nil {
		t.Fatal(err)
	}
	constraint := fmt.Sprintf("test_proof_failure_%d", goal)
	if _, err = d.Exec(fmt.Sprintf(`ALTER TABLE exploration_nodes ADD CONSTRAINT %s CHECK (id<>%d OR state<>'met') NOT VALID`, constraint, goal)); err != nil {
		t.Fatal(err)
	}
	defer d.Exec(`ALTER TABLE exploration_nodes DROP CONSTRAINT ` + constraint)
	if _, _, err = es.ProveGoal(t.Context(), goal, ev, "reason"); err == nil {
		t.Fatal("injected state failure accepted")
	}
	n, err := es.GetNode(goal)
	if err != nil {
		t.Fatal(err)
	}
	if n.State != "open" {
		t.Fatal("state changed on failure")
	}
	var count int
	if err = d.QueryRow(`SELECT count(*) FROM exploration_edges WHERE dst_id=$1 AND rel=$2`, goal, RelProves).Scan(&count); err != nil || count != 0 {
		t.Fatalf("dangling proof after rollback %d %v", count, err)
	}
}
