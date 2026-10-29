package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/Autumn-27/artex/db"
)

func TestCompanyReportHandlersRedactionAndHistoryScope(t *testing.T) {
	s, _ := newRetestServer(t)
	pg := s.m.pg
	company, _, err := pg.Companies().UpsertCompany("handler-report", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pg.Exec(`DELETE FROM companies WHERE id=$1`, company) })
	var asset int64
	if err := pg.QueryRow(`INSERT INTO assets(type,company_id,domain,url) VALUES('subdomain',$1,'handler-report.example.test','https://example.test/?token=handler-secret') RETURNING id`, company).Scan(&asset); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pg.Exec(`DELETE FROM assets WHERE id=$1`, asset) })
	fid, err := pg.AddFinding(0, 0, "logic", "handler finding", "high", "summary", "password=handler-secret", "", []int64{asset})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pg.DeleteFinding(fid) })
	request := func(handler http.HandlerFunc, method, url string, id, version int64) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, url, nil)
		r.SetPathValue("id", strconv.FormatInt(id, 10))
		r.SetPathValue("version", strconv.FormatInt(version, 10))
		w := httptest.NewRecorder()
		handler(w, r)
		return w
	}
	w := request(s.companyReport, "GET", "/?template=technical", company, 0)
	if w.Code != 200 || strings.Contains(w.Body.String(), "handler-secret") || w.Header().Get("X-DITING-Redacted") != "true" || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("default export: %d %s", w.Code, w.Body.String())
	}
	w = request(s.companyReport, "GET", "/?redact=false", company, 0)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "handler-secret") {
		t.Fatal("explicit original copy unavailable")
	}
	w = request(s.companyReport, "GET", "/?redact=bad", company, 0)
	if w.Code != 400 {
		t.Fatal("invalid redact accepted")
	}
	w = request(s.saveCompanyReport, "POST", "/?template=technical&redact=false", company, 0)
	if w.Code != 201 {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	var saved db.CompanyReportVersion
	if err := json.Unmarshal(w.Body.Bytes(), &saved); err != nil {
		t.Fatal(err)
	}
	w = request(s.companyReportVersion, "GET", "/", company, saved.ID)
	if w.Code != 200 || strings.Contains(w.Body.String(), "handler-secret") {
		t.Fatal("history was not redacted")
	}
	w = request(s.companyReportVersion, "GET", "/", company+100000, saved.ID)
	if w.Code != 404 {
		t.Fatal("cross-company history read accepted")
	}
	w = request(s.deleteCompanyReportVersion, "DELETE", "/", company+100000, saved.ID)
	if w.Code != 404 {
		t.Fatal("cross-company deletion accepted")
	}
	w = request(s.assetHistory, "GET", "/", asset, 0)
	if w.Code != 200 || strings.Contains(w.Body.String(), "handler-secret") || !strings.Contains(w.Body.String(), "created") {
		t.Fatalf("asset history: %d %s", w.Code, w.Body.String())
	}
	w = request(s.deleteCompanyReportVersion, "DELETE", "/", company, saved.ID)
	if w.Code != 200 {
		t.Fatal("history deletion failed")
	}
	w = request(s.companyReportVersion, "GET", "/", company, saved.ID)
	if w.Code != 404 {
		t.Fatal("deleted report still accessible")
	}
}
