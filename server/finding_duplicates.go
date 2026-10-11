package server

import (
	"net/http"
	"strconv"
)

func (s *Server) findingDuplicates(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if id <= 0 {
		writeErr(w, 400, "invalid finding ID")
		return
	}
	rel, candidates, err := s.m.pg.FindingDuplicates(r.Context(), id)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"relation": rel, "candidates": candidates})
}
func (s *Server) linkFindingDuplicate(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var b struct {
		Primary int64  `json:"primary_id"`
		Reason  string `json:"reason"`
	}
	if !decodeConversationRequest(w, r, &b) {
		return
	}
	if err := s.m.pg.LinkFindingDuplicate(r.Context(), id, b.Primary, b.Reason); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	s.findingDuplicates(w, r)
}
