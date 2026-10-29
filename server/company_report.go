package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/Autumn-27/artex/report"
)

func (s *Server) companyReport(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt(r, "id")
	if !ok || id <= 0 {
		writeErr(w, 400, "bad company id")
		return
	}
	template := r.URL.Query().Get("template")
	if template == "" {
		template = "technical"
	}
	if template != "technical" && template != "executive" {
		writeErr(w, 400, "unknown report template")
		return
	}
	redacted, ok := exportRedaction(w, r)
	if !ok {
		return
	}
	if r.URL.Query().Get("redact") == "" {
		redacted = true
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
	content := report.CompanyMarkdown(snapshot, template, redacted)
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="company-%d-%s.md"`, id, template))
	if redacted {
		w.Header().Set("X-DITING-Redacted", "true")
	}
	_, _ = w.Write([]byte(content))
}
