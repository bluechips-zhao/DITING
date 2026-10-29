package db

import (
	"fmt"
	"testing"
)

func TestCompanyReportOwnershipAndRetest(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	company := func(name string) int64 {
		id, _, err := d.Companies().UpsertCompany(name, "")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { cleanupCompany(d, id) })
		return id
	}
	a, b := company("report-owner-a"), company("report-owner-b")
	asset := func(owner int64, name string) int64 {
		var id int64
		if err := d.QueryRow(`INSERT INTO assets(type,company_id,domain) VALUES('subdomain',$1,$2) RETURNING id`, owner, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = d.Exec(`DELETE FROM assets WHERE id=$1`, id) })
		return id
	}
	aa, ab := asset(a, "report-a.example.test"), asset(b, "report-b.example.test")
	task, err := d.CreateTask("shared task contains other company's context", "", nil, 600, 60)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.DeleteTask(task.ID) })
	finding := func(assets []int64) int64 {
		id, err := d.AddFinding(task.ID, 0, "logic", "report finding", "high", "summary", "evidence", "", assets)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _, _ = d.DeleteFinding(id) })
		return id
	}
	own, other := finding([]int64{aa}), finding([]int64{ab})
	finding([]int64{aa, ab})
	finding([]int64{aa, 9223372036854775807})
	finding(nil)
	if _, err := d.Exec(`INSERT INTO finding_duplicates(finding_id,primary_id,reason,previous_status) VALUES($1,$2,'private other-company context','pending')`, own, other); err != nil {
		t.Fatal(err)
	}
	var rid int64
	for _, version := range []int{3, 4} {
		if err := d.QueryRow(`INSERT INTO finding_retests(finding_id,status,verdict,snapshot) VALUES($1,'completed','fixed',$2::jsonb) RETURNING id`, own, fmt.Sprintf(`{"finding":{"evidence_version":%d}}`, version)).Scan(&rid); err != nil {
			t.Fatal(err)
		}
	}
	out, err := d.CompanyReport(t.Context(), a)
	if err != nil {
		t.Fatal(err)
	}
	if out == nil || out.AssetCounts["subdomain"] != 1 || out.MixedExcluded != 2 || len(out.Findings) != 1 {
		t.Fatalf("incorrect scope: %+v", out)
	}
	f := out.Findings[0]
	if f.ID != own || f.TaskID == nil || *f.TaskID != task.ID || f.TaskDescription != "" || f.DuplicateOf != nil || f.DuplicateReason != "" {
		t.Fatalf("incorrect provenance or scope: %+v", f)
	}
	if len(out.Tasks) != 1 || out.Tasks[0].ID != task.ID {
		t.Fatalf("tasks: %+v", out.Tasks)
	}
	if len(out.Retests) != 1 || out.Retests[0].ID != rid || out.Retests[0].BaselineVersion == nil || *out.Retests[0].BaselineVersion != 4 {
		t.Fatalf("retests: %+v", out.Retests)
	}
	missing, err := d.CompanyReport(t.Context(), 9223372036854775807)
	if err != nil || missing != nil {
		t.Fatalf("missing company: %+v %v", missing, err)
	}
}
