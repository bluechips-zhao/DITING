package server

import (
	"net/http"
	"path/filepath"

	"github.com/Autumn-27/artex/bootstrap"
)

func (s *Server) environmentStatus(w http.ResponseWriter, r *http.Request) {
	var version string
	if err := s.m.pg.QueryRowContext(r.Context(), `SELECT current_setting('server_version')`).Scan(&version); err != nil {
		writeJSON(w, 503, map[string]string{"error": "数据库暂时不可用，请检查服务状态后重试"})
		return
	}
	ready := s.m.pg.CredentialKeyReady(filepath.Join(s.keyDir, "secrets.key"))
	writeJSON(w, 200, map[string]any{"environment": bootstrap.Inspect(s.m.dir, s.keyDir), "database_version": version, "credential_key_ready": ready})
}
