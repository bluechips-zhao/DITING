package report

import (
	"encoding/csv"
	"encoding/json"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/db"
)

func TestRedactedCSVNeutralizesSpreadsheetFormulas(t *testing.T) {
	f := &db.DBFinding{ID: 42, Name: "\t=HYPERLINK(\"https://example.test\")", Summary: "+cmd"}
	raw := strings.TrimPrefix(string(FindingsCSV([]*db.DBFinding{f})), "\uFEFF")
	rows, err := csv.NewReader(strings.NewReader(raw)).ReadAll()
	if err != nil || len(rows) != 2 || rows[1][0] != "42" {
		t.Fatal("CSV format or source ID changed")
	}
	if !strings.HasPrefix(rows[1][1], "'") || rows[1][7] != "'+cmd" || f.Summary != "+cmd" {
		t.Fatal("spreadsheet formula not escaped, or original mutated")
	}
}

func TestRedactRecognizedCredentialsAndPreserveEvidenceReferences(t *testing.T) {
	input := "Authorization: Bearer fixture-bearer\nCookie: sid=fixture-cookie; other=fixture-other\n" +
		`{"password":"fixture-password","api_key":"fixture-api"}` + "\n" +
		"https://fixture-user:fixture-pass@example.test/proof?api%5Fkey=fixture-url&case=42"
	out := RedactText(input)
	for _, secret := range []string{"fixture-bearer", "fixture-cookie", "fixture-other", "fixture-password", "fixture-api", "fixture-user", "fixture-pass", "fixture-url"} {
		if strings.Contains(out, secret) {
			t.Errorf("credential form remained in redacted text")
		}
	}
	if !strings.Contains(out, "example.test/proof") || !strings.Contains(out, "case=42") {
		t.Fatal("non-secret evidence context removed")
	}
}

func TestRedactJSONPreservesPrecisionAndInput(t *testing.T) {
	input := map[string]any{"source_id": int64(9007199254740993), "config": map[string]any{"api_key": "fixture-secret"}}
	masked, err := RedactJSON(input)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(masked)
	if err != nil || !strings.Contains(string(encoded), "9007199254740993") || strings.Contains(string(encoded), "fixture-secret") {
		t.Fatal("invalid redacted JSON or precision loss")
	}
	if input["config"].(map[string]any)["api_key"] != "fixture-secret" {
		t.Fatal("original input mutated")
	}
}

func TestRedactedFindingsDoNotOverwriteOriginalEvidence(t *testing.T) {
	f := &db.DBFinding{ID: 7, Evidence: "password=fixture-original", AssetIDs: []int64{9}, TrafficBindings: []db.FindingTrafficBinding{{ID: 3, Note: "token=fixture-note", Snapshot: db.TrafficEvidenceSnapshot{URL: "https://example.test/?token=fixture-url", ReqHead: "Cookie: fixture-raw"}}}}
	redacted := RedactedFindings([]*db.DBFinding{f})[0]
	if redacted.ID != 7 || redacted.TrafficBindings[0].ID != 3 || redacted.TrafficBindings[0].Snapshot.ReqHead != "" {
		t.Fatal("source identity or redaction incorrect")
	}
	redacted.AssetIDs[0] = 10
	if f.Evidence != "password=fixture-original" || f.AssetIDs[0] != 9 || f.TrafficBindings[0].Snapshot.ReqHead != "Cookie: fixture-raw" {
		t.Fatal("original evidence mutated")
	}
}
