package db

import (
	"fmt"
	"testing"
	"time"
)

func TestListTasksBulkHydratesTaskContext(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) - skipping", err)
	}
	defer d.Close()

	stamp := time.Now().UnixNano()
	profileID, err := d.SaveProfile(&LLMProfile{
		Name: fmt.Sprintf("bulk-list-profile-%d", stamp), Format: "openai", Model: "test", APIKey: "test-key",
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.DeleteProfile(profileID) })
	source, err := d.CreateTask(fmt.Sprintf("bulk-list-source-%d", stamp), "goal", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.DeleteTask(source.ID) })
	companyID, _, err := d.Companies().UpsertCompany(fmt.Sprintf("Bulk List Company %d", stamp), "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Companies().DeleteCompany(companyID) })
	child, err := d.CreateTaskWithOptions("bulk-list-child", "goal", TaskCreateOptions{
		LLMProfileIDs: []int64{profileID},
		SourceTaskIDs: []int64{source.ID},
		CompanyIDs:    []int64{companyID},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.DeleteTask(child.ID) })

	tasks, err := d.ListTasks()
	if err != nil {
		t.Fatal(err)
	}
	var got *Task
	for _, task := range tasks {
		if task.ID == child.ID {
			got = task
			break
		}
	}
	if got == nil {
		t.Fatalf("task %d missing from list", child.ID)
	}
	if len(got.LLMProfileIDs) != 1 || got.LLMProfileIDs[0] != profileID ||
		got.ActiveLLMProfileID == nil || *got.ActiveLLMProfileID != profileID || got.LLMFailoverState != "ready" {
		t.Fatalf("LLM context was not bulk hydrated: %+v", got)
	}
	if len(got.SourceTaskIDs) != 1 || got.SourceTaskIDs[0] != source.ID {
		t.Fatalf("source task context was not bulk hydrated: %v", got.SourceTaskIDs)
	}
	if len(got.CompanyIDs) != 1 || got.CompanyIDs[0] != companyID {
		t.Fatalf("company context was not bulk hydrated: %v", got.CompanyIDs)
	}
}

func TestTaskListMetricsAll(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) - skipping", err)
	}
	defer d.Close()

	task, err := d.CreateTask("task-list-metrics", "goal", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.DeleteTask(task.ID) })

	resultAt := time.Now().Add(-time.Second).Truncate(time.Second)
	latestAt := resultAt.Add(time.Second)
	if _, err := d.Exec(`
INSERT INTO activity(
    exploration_id, kind, input_tokens, output_tokens,
    cache_read_tokens, cache_write_tokens, created_at
) VALUES ($1,'result',11,7,3,2,$2), ($1,'tool_result',999,999,999,999,$3)`,
		task.ExplorationID, resultAt, latestAt); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`
INSERT INTO exploration_nodes(exploration_id, kind, payload, state)
VALUES ($1,'goal','{}','met'), ($1,'goal','{}','open')`, task.ExplorationID); err != nil {
		t.Fatal(err)
	}

	all, err := d.TaskListMetricsAll()
	if err != nil {
		t.Fatal(err)
	}
	metrics, ok := all[task.ExplorationID]
	if !ok {
		t.Fatalf("metrics for exploration %d missing", task.ExplorationID)
	}
	if metrics.Tokens.InputTokens != 11 || metrics.Tokens.OutputTokens != 7 ||
		metrics.Tokens.CacheReadTokens != 3 || metrics.Tokens.CacheWriteTokens != 2 {
		t.Fatalf("unexpected token metrics: %+v", metrics.Tokens)
	}
	if metrics.LastActivity != latestAt.Unix() {
		t.Fatalf("last activity=%d, want %d", metrics.LastActivity, latestAt.Unix())
	}
	if metrics.Goals.Total != 2 || metrics.Goals.Met != 1 {
		t.Fatalf("unexpected goal metrics: %+v", metrics.Goals)
	}
}

func TestTaskListFindingMetricsSkewAndIsolation(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) - skipping", err)
	}
	defer d.Close()
	marker := fmt.Sprintf("finding-metrics-%d", time.Now().UnixNano())
	t.Cleanup(func() { _, _ = d.Exec(`DELETE FROM findings WHERE vulnclass=$1`, marker) })
	tasks := make([]*Task, 4)
	for i := range tasks {
		tasks[i], err = d.CreateTask(marker, "goal", nil, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		id := tasks[i].ID
		t.Cleanup(func() { _ = d.DeleteTask(id) })
	}
	if _, err := d.Exec(`INSERT INTO findings(task_id,vulnclass,severity)
		SELECT $1,$2,'low' FROM generate_series(1,5000)`, tasks[0].ID, marker); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO findings(task_id,vulnclass,severity) VALUES
		($1,$4,'critical'),($1,$4,'high'),($1,$4,'medium'),($1,$4,'unknown'),
		($2,$4,'high'),($3,$4,'critical'),(NULL,$4,'critical')`,
		tasks[0].ID, tasks[1].ID, tasks[3].ID, marker); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`UPDATE tasks SET deleted_at=now() WHERE id=$1`, tasks[3].ID); err != nil {
		t.Fatal(err)
	}
	all, err := d.TaskListMetricsAll()
	if err != nil {
		t.Fatal(err)
	}
	want := []FindingSeverityCounts{
		{Critical: 1, High: 1, Medium: 1, Low: 5000},
		{High: 1},
		{},
	}
	for i, expected := range want {
		got, ok := all[tasks[i].ExplorationID]
		if !ok || got.Findings != expected {
			t.Errorf("task %d findings=%+v present=%v, want %+v", i, got.Findings, ok, expected)
		}
	}
	if _, ok := all[tasks[3].ExplorationID]; ok {
		t.Error("soft-deleted task leaked into live metrics")
	}
}
