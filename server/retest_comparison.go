package server

import "net/http"

func (s *Server) findingRetestComparison(w http.ResponseWriter, r *http.Request) {
	pg := s.pg(w)
	if pg == nil {
		return
	}
	findingID, ok := pathInt(r, "id")
	if !ok || findingID <= 0 {
		writeErr(w, 400, "bad finding id")
		return
	}
	retestID, ok := pathInt(r, "retest")
	if !ok || retestID <= 0 {
		writeErr(w, 400, "bad retest id")
		return
	}
	comparison, err := pg.FindingRetestComparison(r.Context(), findingID, retestID)
	if err != nil {
		writeErr(w, 500, "读取复测对比失败")
		return
	}
	if comparison == nil {
		writeErr(w, 404, "retest not found for this finding")
		return
	}
	writeJSON(w, 200, comparison)
}
