package server

import (
	"net/http"
	"strconv"

	"github.com/Autumn-27/artex/report"
)

func (s *Server) assetHistory(w http.ResponseWriter, r *http.Request) {
	id, ok := pathInt(r, "id")
	if !ok || id <= 0 {
		writeErr(w, 400, "bad asset id")
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
	items, err := pg.AssetHistory(r.Context(), id, before)
	if err != nil {
		writeErr(w, 500, "读取资产变化失败")
		return
	}
	// Projection excludes credential containers; values such as URLs still need
	// redaction. Marshal the complete response to avoid converting bigint IDs.
	safe, err := report.RedactJSON(items)
	if err != nil {
		writeErr(w, 500, "处理资产变化失败")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, safe)
}
