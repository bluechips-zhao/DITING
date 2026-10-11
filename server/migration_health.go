package server

import "net/http"

func (s *Server) migrationHealth(w http.ResponseWriter, r *http.Request) {
	rows, err := s.m.pg.QueryContext(r.Context(), `SELECT flag,state,error,updated_at FROM migration_health ORDER BY flag`)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var flag, state, message, at string
		if err = rows.Scan(&flag, &state, &message, &at); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
		out = append(out, map[string]any{"flag": flag, "state": state, "error": message, "updated_at": at})
	}
	if err = rows.Err(); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, out)
}
