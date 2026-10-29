package server

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/Autumn-27/artex/report"
)

func (s *Server) saveCompanyReport(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt(r, "id")
	if !ok || id <= 0 {
		writeErr(w, 400, "bad company id")
		return
	}
	template := r.URL.Query().Get("template")
	if template != "technical" && template != "executive" {
		writeErr(w, 400, "unknown report template")
		return
	}
	pg := s.pg(w)
	if pg == nil {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	snapshot, err := pg.CompanyReport(ctx, id)
	if err != nil {
		writeErr(w, 500, "生成企业报告失败")
		return
	}
	if snapshot == nil {
		writeErr(w, 404, "company not found")
		return
	}
	// The server renders its own stored records. Clients cannot persist arbitrary
	// unredacted report bodies or override the snapshot timestamp.
	v, err := pg.SaveCompanyReport(ctx, id, template, snapshot.GeneratedAt, report.CompanyMarkdown(snapshot, template, true))
	if err != nil {
		writeErr(w, 500, "保存报告版本失败")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 201, v)
}

func (s *Server) companyReportHistory(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt(r, "id")
	if !ok || id <= 0 {
		writeErr(w, 400, "bad company id")
		return
	}
	var before int64
	if raw := r.URL.Query().Get("before"); raw != "" {
		var err error
		before, err = strconv.ParseInt(raw, 10, 64)
		if err != nil || before <= 0 {
			writeErr(w, 400, "bad history cursor")
			return
		}
	}
	pg := s.pg(w)
	if pg == nil {
		return
	}
	items, err := pg.CompanyReportHistory(r.Context(), id, before)
	if err != nil {
		writeErr(w, 500, "读取报告历史失败")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, items)
}

func (s *Server) companyReportVersion(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt(r, "id")
	version, valid := pathInt(r, "version")
	if !ok || !valid || id <= 0 || version <= 0 {
		writeErr(w, 400, "bad report id")
		return
	}
	pg := s.pg(w)
	if pg == nil {
		return
	}
	v, err := pg.CompanyReportVersion(r.Context(), id, version)
	if err != nil {
		writeErr(w, 500, "读取报告版本失败")
		return
	}
	if v == nil {
		writeErr(w, 404, "report not found for this company")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, v)
}

func (s *Server) deleteCompanyReportVersion(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt(r, "id")
	version, valid := pathInt(r, "version")
	if !ok || !valid || id <= 0 || version <= 0 {
		writeErr(w, 400, "bad report id")
		return
	}
	pg := s.pg(w)
	if pg == nil {
		return
	}
	deleted, err := pg.DeleteCompanyReportVersion(r.Context(), id, version)
	if err != nil {
		writeErr(w, 500, "删除报告版本失败")
		return
	}
	if !deleted {
		writeErr(w, 404, "report not found for this company")
		return
	}
	writeJSON(w, 200, map[string]bool{"deleted": true})
}
