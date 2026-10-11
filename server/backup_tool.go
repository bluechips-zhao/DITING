package server

import (
	"github.com/Autumn-27/artex/scripts"
	"net/http"
)

func (s *Server) backupTool(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="diting-backup.ps1"`)
	_, _ = w.Write(scripts.BackupTool)
}
