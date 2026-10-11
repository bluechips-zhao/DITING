package db

import (
	"context"
	"sync"
	"testing"
)

func TestDuplicateRelationshipsAndReportVersionJobs(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	task, err := d.CreateTask("relationship test", "local", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer d.DeleteTask(task.ID)
	store := d.Exploration(task.ExplorationID)
	in := RecordFindingInput{TaskID: task.ID, Severity: "medium", Summary: "original", VulnClass: "test-class", Evidence: "same local proof"}
	a, err := store.RecordFinding(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	defer d.DeleteFinding(a.FindingID)
	in.Summary = "other"
	b, err := store.RecordFinding(t.Context(), in)
	if err != nil {
		t.Fatal(err)
	}
	defer d.DeleteFinding(b.FindingID)
	_, candidates, err := d.FindingDuplicates(t.Context(), b.FindingID)
	if err != nil || len(candidates) < 1 {
		t.Fatalf("candidate evidence missing %v", err)
	}
	if err = d.LinkFindingDuplicate(t.Context(), b.FindingID, a.FindingID, "same endpoint/proof reviewed"); err != nil {
		t.Fatal(err)
	}
	linked, _ := d.GetFinding(b.FindingID)
	if linked.DuplicateOf == nil || *linked.DuplicateOf != a.FindingID {
		t.Fatal("relationship absent from detail/export")
	}
	if _, _, _, e := d.SetFindingStatusWithNotify(t.Context(), b.FindingID, "confirmed"); e == nil {
		t.Fatal("status bypassed relationship")
	}
	if err = d.LinkFindingDuplicate(t.Context(), a.FindingID, b.FindingID, "cycle"); err == nil {
		t.Fatal("cycle accepted")
	}
	if err = d.LinkFindingDuplicate(t.Context(), b.FindingID, 0, "different endpoints after review"); err != nil {
		t.Fatal(err)
	}
	f, _ := d.GetFinding(b.FindingID)
	if f.Status != "pending" {
		t.Fatal("original triage lost")
	}
	var wg sync.WaitGroup
	ids := make(chan int64, 5)
	for i := 0; i < 5; i++ {
		wg.Go(func() {
			j, _, e := d.QueueFindingReport(context.Background(), a.FindingID)
			if e != nil {
				t.Error(e)
			} else {
				ids <- j.ID
			}
		})
	}
	wg.Wait()
	close(ids)
	var jobID int64
	for id := range ids {
		if jobID != 0 && jobID != id {
			t.Fatal("parallel report jobs created")
		}
		jobID = id
	}
	version := int64(0)
	if _, err = d.SetFindingReportVersionByNodeID(t.Context(), a.NodeID, "old report", &version); err != nil {
		t.Fatal(err)
	}
	if err = d.FinishFindingReport(t.Context(), jobID, ""); err != nil {
		t.Fatal(err)
	}
	jobs, versions, err := d.FindingReportStatus(t.Context(), a.FindingID)
	if err != nil || jobs[0].State != "ready" || len(versions) != 1 {
		t.Fatalf("report not ready %v %+v", err, jobs)
	}
	if _, err = d.SetFindingReportVersionByNodeID(t.Context(), a.NodeID, "new report", &version); err != nil {
		t.Fatal(err)
	}
	_, versions, err = d.FindingReportStatus(t.Context(), a.FindingID)
	if err != nil || len(versions) != 2 || versions[1].Report != "old report" {
		t.Fatal("old report lost")
	}
	version = 1
	if _, err = d.SetFindingReportVersionByNodeID(t.Context(), a.NodeID, "stale write", &version); err == nil {
		t.Fatal("wrong evidence version accepted")
	}
	f, _ = d.GetFinding(a.FindingID)
	if f.Report != "new report" {
		t.Fatal("stale write overwrote current report")
	}
}
