package db

import (
	"testing"
	"time"
)

func TestCompanyReportHistoryIsolationAndImmutableCopy(t *testing.T) {
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
	a, b := company("history-a"), company("history-b")
	ctx := t.Context()
	stamp := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	v1, err := d.SaveCompanyReport(ctx, a, "technical", stamp, "first redacted copy")
	if err != nil {
		t.Fatal(err)
	}
	v2, err := d.SaveCompanyReport(ctx, a, "executive", stamp, "second redacted copy")
	if err != nil {
		t.Fatal(err)
	}
	items, err := d.CompanyReportHistory(ctx, a, 0)
	if err != nil || len(items) != 2 || items[0].ID != v2.ID || items[0].Content != "" {
		t.Fatalf("history: %+v %v", items, err)
	}
	page, err := d.CompanyReportHistory(ctx, a, v2.ID)
	if err != nil || len(page) != 1 || page[0].ID != v1.ID {
		t.Fatalf("page: %+v %v", page, err)
	}
	if _, err := d.Exec(`UPDATE companies SET name='renamed' WHERE id=$1`, a); err != nil {
		t.Fatal(err)
	}
	old, err := d.CompanyReportVersion(ctx, a, v1.ID)
	if err != nil || old == nil || old.Content != "first redacted copy" || !old.SnapshotAt.Equal(stamp) {
		t.Fatalf("snapshot changed: %+v %v", old, err)
	}
	wrong, err := d.CompanyReportVersion(ctx, b, v1.ID)
	if err != nil || wrong != nil {
		t.Fatalf("cross-company read: %+v %v", wrong, err)
	}
	deleted, err := d.DeleteCompanyReportVersion(ctx, b, v1.ID)
	if err != nil || deleted {
		t.Fatalf("cross-company delete: %v %v", deleted, err)
	}
	deleted, err = d.DeleteCompanyReportVersion(ctx, a, v1.ID)
	if err != nil || !deleted {
		t.Fatal("own deletion failed", err)
	}
	old, err = d.CompanyReportVersion(ctx, a, v1.ID)
	if err != nil || old != nil {
		t.Fatal("deleted copy still visible", err)
	}
	cleanupCompany(d, a)
	items, err = d.CompanyReportHistory(ctx, a, 0)
	if err != nil || len(items) != 0 {
		t.Fatal("company deletion retained history", err)
	}
}
