package server

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"github.com/Autumn-27/artex/db"
	"net/http"
	"os"
	"strconv"
)

func stagedDownload(w http.ResponseWriter, r *http.Request, name, contentType string, write func(*os.File) error) {
	f, err := os.CreateTemp("", "diting-export-*")
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	defer func() { f.Close(); os.Remove(f.Name()) }()
	if err = write(f); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if _, err = f.Seek(0, 0); err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	info, err := f.Stat()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, name))
	http.ServeContent(w, r, name, info.ModTime(), f)
}

func (s *Server) exportAssets(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	format := q.Get("format")
	if format == "" {
		format = "json"
	}
	if format != "csv" && format != "json" {
		writeErr(w, 400, "format must be csv or json")
		return
	}
	if err := db.ValidateDSL(q.Get("dsl")); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	task, _ := strconv.ParseInt(q.Get("task_id"), 10, 64)
	company, _ := strconv.ParseInt(q.Get("company_id"), 10, 64)
	stagedDownload(w, r, "assets."+format, map[string]string{"csv": "text/csv; charset=utf-8", "json": "application/json"}[format], func(f *os.File) error {
		enc := json.NewEncoder(f)
		cw := csv.NewWriter(f)
		first := true
		if format == "json" {
			if _, err := f.WriteString("["); err != nil {
				return err
			}
		} else {
			if err := cw.Write([]string{"id", "type", "company_id", "task_ids", "asset_json"}); err != nil {
				return err
			}
		}
		err := s.m.Assets().VisitAssetsExport(r.Context(), q.Get("dsl"), q.Get("type"), task, company, func(a *db.Asset) error {
			if format == "json" {
				if !first {
					if _, e := f.WriteString(","); e != nil {
						return e
					}
				}
				first = false
				return enc.Encode(a)
			}
			raw, e := json.Marshal(a)
			if e != nil {
				return e
			}
			tasks, e := json.Marshal(a.TaskIDs)
			if e != nil {
				return e
			}
			company := ""
			if a.CompanyID != nil {
				company = fmt.Sprint(*a.CompanyID)
			}
			return cw.Write([]string{fmt.Sprint(a.ID), a.Type, company, string(tasks), string(raw)})
		})
		if err != nil {
			return err
		}
		if format == "json" {
			_, err = f.WriteString("]\n")
		} else {
			cw.Flush()
			err = cw.Error()
		}
		return err
	})
}
