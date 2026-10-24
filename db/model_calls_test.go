package db

import "testing"

func TestDirectionStatsPreserveCallsAndCountActualOutputs(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	task, err := d.CreateTask("stats", "local", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer d.DeleteTask(task.ID)
	other, err := d.CreateTask("foreign stats", "local", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer d.DeleteTask(other.ID)
	s := d.Exploration(task.ExplorationID)
	intent, err := s.AddIntent(map[string]any{"summary": "one direction"}, 1, nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := d.Exploration(other.ExplorationID).AddIntent(nil, 1, nil, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.StartModelCall(t.Context(), task.ID, foreign); err == nil {
		t.Fatal("foreign intent attribution accepted")
	}
	call, err := d.StartModelCall(t.Context(), task.ID, intent)
	if err != nil {
		t.Fatal(err)
	}
	if err = d.FinishModelCall(t.Context(), call, 120, 20, 10, "completed", true); err != nil {
		t.Fatal(err)
	}
	if err = d.FinishModelCall(t.Context(), call, 999, 999, 999, "failed", false); err != nil {
		t.Fatal(err)
	}
	call, err = d.StartModelCall(t.Context(), task.ID, intent)
	if err != nil {
		t.Fatal(err)
	}
	if err = d.FinishModelCall(t.Context(), call, 80, 7, 0, "failed", false); err != nil {
		t.Fatal(err)
	}
	if _, err = d.StartModelCall(t.Context(), task.ID, 0); err != nil {
		t.Fatal(err)
	}
	for _, v := range []struct{ kind, state string }{{"fact", "confirmed"}, {"finding", "confirmed"}, {"finding", "candidate"}, {"finding", "dismissed"}} {
		id, err := s.AddNode(v.kind, nil, 0, v.state, "test", nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = d.Exec(`INSERT INTO exploration_edges(exploration_id,src_id,rel,dst_id) VALUES($1,$2,'yields',$3)`, task.ExplorationID, intent, id); err != nil {
			t.Fatal(err)
		}
	}
	for _, isErr := range []bool{false, true} {
		if _, err = s.AppendActivity(Activity{NodeID: &intent, Kind: "tool_result", Tool: "report_finding", IsError: isErr}); err != nil {
			t.Fatal(err)
		}
	}
	if err = d.RecoverInterruptedModelCalls(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = d.RecoverInterruptedModelCalls(t.Context()); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(testDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	stats, err := reopened.DirectionExecutionStats(t.Context(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stats) != 2 {
		t.Fatalf("directions=%+v", stats)
	}
	for _, v := range stats {
		if v.IntentID == 0 {
			if v.ModelCalls != 1 || v.ModelInterrupted != 1 || v.UnknownUsageCalls != 1 || v.ModelElapsedMS != 0 || v.UnknownElapsedCalls != 1 {
				t.Fatalf("restart stats=%+v", v)
			}
			continue
		}
		if v.IntentID != intent || v.ModelCalls != 2 || v.ModelFailed != 1 || v.ModelElapsedMS != 200 || v.InputTokens != 27 || v.OutputTokens != 10 || v.UnknownUsageCalls != 1 || v.ToolResults != 2 || v.ToolErrors != 1 || v.ConfirmedFacts != 1 || v.ConfirmedFindings != 1 || v.CandidateFindings != 1 {
			t.Fatalf("direction stats=%+v", v)
		}
	}
}
