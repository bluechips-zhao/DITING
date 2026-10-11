package report

import (
	"github.com/Autumn-27/artex/db"
	"strings"
	"testing"
	"time"
)

func TestTaskReportIncludesTriageAndDetailedEvidence(t *testing.T) {
	md := Markdown(Input{Title: "scope test", GeneratedAt: time.Now(), AssetCounts: map[string]int{"ip": 2}, Records: []*db.DBFinding{
		{ID: 1, Status: db.FindingPending, Severity: "high", Summary: "candidate", Evidence: "local proof", Report: "## Reproduction\nSteps"},
		{ID: 2, Status: db.FindingConfirmed, Severity: "low", Summary: "confirmed"},
		{ID: 3, Status: db.FindingFalsePositive, Severity: "medium", Summary: "false alarm"},
	}})
	for _, text := range []string{"confirmed：1", "pending：1", "false_positive：1", "本任务关联资产", "ip 2", "local proof", "Reproduction", "false_positive"} {
		if !strings.Contains(md, text) {
			t.Errorf("missing %q in %s", text, md)
		}
	}
}
