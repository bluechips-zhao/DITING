package report

import (
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
)

func TestCompanyMarkdownTemplatesAndRedaction(t *testing.T) {
	taskID, baseline := int64(73), int64(2)
	s := &db.CompanyReportSnapshot{
		CompanyID: 42, CompanyName: "Example | Company", GeneratedAt: time.Now(),
		AssetCounts: map[string]int{"subdomain": 1},
		Tasks:       []db.CompanyReportTask{{ID: taskID, Status: "paused"}},
		Findings:    []*db.DBFinding{{ID: 91, TaskID: &taskID, AssetIDs: []int64{24}, Name: "sample", Status: "pending", Evidence: "Authorization: Bearer company-test-secret", EvidenceVersion: 3, ReportEvidenceVersion: 1}},
		Retests:     []db.CompanyReportRetest{{ID: 107, FindingID: 91, Status: "running", Verdict: "fixed", BaselineVersion: &baseline}},
	}
	technical := CompanyMarkdown(s, "technical", true)
	for _, wanted := range []string{"42", "91", "73", "24", "107", "尚无完成结论", "Example \\| Company"} {
		if !strings.Contains(technical, wanted) {
			t.Fatalf("missing %q", wanted)
		}
	}
	if strings.Contains(technical, "company-test-secret") || strings.Contains(technical, "| 已修复 |") {
		t.Fatal("credential leaked or unfinished result called fixed")
	}
	if !strings.Contains(s.Findings[0].Evidence, "company-test-secret") {
		t.Fatal("original changed")
	}
	raw := CompanyMarkdown(s, "technical", false)
	if !strings.Contains(raw, "company-test-secret") {
		t.Fatal("explicit original copy changed")
	}
	executive := CompanyMarkdown(s, "executive", true)
	if strings.Contains(executive, "Authorization:") || !strings.Contains(executive, "发现概览") {
		t.Fatal("executive template contains detailed evidence or lacks overview")
	}
}
