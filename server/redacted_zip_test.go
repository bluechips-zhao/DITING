package server

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Autumn-27/artex/db"
)

func TestRedactedZIPOmitsRawBodiesWithoutReadingThem(t *testing.T) {
	f := &db.DBFinding{ID: 42, Name: "share fixture", Evidence: "password=fixture-evidence", TrafficBindings: []db.FindingTrafficBinding{{ID: 9, FindingID: 42, Note: "token=fixture-note", Snapshot: db.TrafficEvidenceSnapshot{URL: "https://example.test/?token=fixture-url", ReqHead: "Cookie: fixture-cookie", RespHead: "Set-Cookie: fixture-response"}}}}
	var output bytes.Buffer
	// No body files exist. A successful export must not try to open original bytes.
	if err := writeFindingsEvidenceZip(&output, []*db.DBFinding{f}, t.TempDir(), time.Now(), true); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(output.Bytes()), int64(output.Len()))
	if err != nil {
		t.Fatal(err)
	}
	info, manifest := false, false
	for _, entry := range archive.File {
		if strings.HasSuffix(entry.Name, ".http") || strings.HasSuffix(entry.Name, ".bin") {
			t.Fatal("raw attachment included")
		}
		info = info || entry.Name == "EXPORT-INFO.txt"
		manifest = manifest || entry.Name == "evidence/42/9/manifest.json"
		r, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
		for _, secret := range []string{"fixture-evidence", "fixture-note", "fixture-url", "fixture-cookie", "fixture-response"} {
			if bytes.Contains(data, []byte(secret)) {
				t.Fatal("credential remained in shared ZIP")
			}
		}
	}
	if !info || !manifest || f.Evidence != "password=fixture-evidence" {
		t.Fatal("missing provenance or original changed")
	}
}
