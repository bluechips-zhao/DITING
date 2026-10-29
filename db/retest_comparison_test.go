package db

import "testing"

func TestRetestComparisonImmutableBaselineAndPairScope(t *testing.T) {
	d, fid := retestDB(t)
	r, _, _, err := d.CreateFindingRetest(t.Context(), fid, "")
	if err != nil {
		t.Fatal(err)
	}
	before, err := d.FindingRetestComparison(t.Context(), fid, r.ID)
	if err != nil || before == nil || before.Baseline == nil || before.Baseline.Evidence != "original evidence" || before.SourceEvidenceChanged == nil || *before.SourceEvidenceChanged {
		t.Fatalf("baseline: %+v %v", before, err)
	}
	if _, err := d.Exec(`UPDATE findings SET evidence='new evidence',evidence_version=evidence_version+1 WHERE id=$1`, fid); err != nil {
		t.Fatal(err)
	}
	after, err := d.FindingRetestComparison(t.Context(), fid, r.ID)
	if err != nil || after == nil || after.Baseline.Evidence != "original evidence" || after.SourceEvidenceChanged == nil || !*after.SourceEvidenceChanged {
		t.Fatalf("changed baseline: %+v %v", after, err)
	}
	wrong, err := d.FindingRetestComparison(t.Context(), fid+100000, r.ID)
	if err != nil || wrong != nil {
		t.Fatal("cross-finding pair returned", err)
	}
	if _, err := d.Exec(`UPDATE finding_retests SET snapshot='{}' WHERE id=$1`, r.ID); err != nil {
		t.Fatal(err)
	}
	legacy, err := d.FindingRetestComparison(t.Context(), fid, r.ID)
	if err != nil || legacy == nil || legacy.Baseline != nil || legacy.SourceEvidenceChanged != nil {
		t.Fatalf("fabricated legacy baseline: %+v %v", legacy, err)
	}
}
