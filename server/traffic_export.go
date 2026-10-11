package server

import (
	"archive/zip"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"github.com/Autumn-27/artex/traffic"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

func (s *Server) exportTraffic(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	format := q.Get("format")
	if format == "" {
		format = "har"
	}
	if format != "har" && format != "csv" && format != "zip" {
		writeErr(w, 400, "format must be har, csv or zip")
		return
	}
	ids := []string{}
	if raw := q.Get("ids"); raw != "" {
		ids = strings.Split(raw, ",")
		if len(ids) > 1000 {
			writeErr(w, 400, "at most 1000 selected IDs")
			return
		}
	}
	f := traffic.PageQuery{Host: q.Get("host"), Method: q.Get("method"), Query: q.Get("q"), Body: q.Get("body"), Path: q.Get("path"), Status: q.Get("status"), RespMin: int64(atoiDefault(q.Get("resp_min"), -1)), RespMax: int64(atoiDefault(q.Get("resp_max"), -1)), Sort: q.Get("sort"), Order: q.Get("order")}
	stagedDownload(w, r, "traffic."+format, map[string]string{"har": "application/json", "csv": "text/csv; charset=utf-8", "zip": "application/zip"}[format], func(out *os.File) error {
		cw := csv.NewWriter(out)
		var zw *zip.Writer
		first := true
		if format == "zip" {
			zw = zip.NewWriter(out)
		} else if format == "csv" {
			if err := cw.Write([]string{"id", "captured_at", "method", "url", "status", "content_type", "response_bytes"}); err != nil {
				return err
			}
		} else {
			if _, err := io.WriteString(out, `{"log":{"version":"1.2","creator":{"name":"DITING","version":"1"},"comment":"未采集网络 timing；time/timings 使用 -1。正文按完整原始字节导出为 base64；请求正文使用扩展字段 _encoding。","entries":[`); err != nil {
				return err
			}
		}
		err := s.m.Traffic().VisitExport(r.Context(), f, ids, func(m traffic.ExchangeMeta, e *traffic.EvidenceExchange) error {
			if format == "csv" {
				return cw.Write([]string{m.ID, time.Unix(m.TS, 0).UTC().Format(time.RFC3339), m.Method, m.URL, strconv.Itoa(m.Status), m.ContentType, strconv.Itoa(m.RespLen)})
			}
			if format == "zip" {
				name := base64.RawURLEncoding.EncodeToString([]byte(m.ID))
				for _, part := range []struct {
					name, head string
					body       io.Reader
				}{{"request", e.ReqHead, e.Request}, {"response", e.RespHead, e.Response}} {
					w, err := zw.Create(name + "/" + part.name + ".http")
					if err != nil {
						return err
					}
					if _, err = io.WriteString(w, strings.TrimRight(part.head, "\r\n")+"\r\n\r\n"); err != nil {
						return err
					}
					if _, err = io.Copy(w, part.body); err != nil {
						return err
					}
				}
				w, err := zw.Create(name + "/metadata.json")
				if err != nil {
					return err
				}
				return json.NewEncoder(w).Encode(m)
			}
			if !first {
				if _, err := io.WriteString(out, ","); err != nil {
					return err
				}
			}
			first = false
			return writeHAREntry(out, *e)
		}, format != "csv")
		if err != nil {
			if zw != nil {
				_ = zw.Close()
			}
			return err
		}
		if format == "zip" {
			return zw.Close()
		}
		if format == "csv" {
			cw.Flush()
			return cw.Error()
		}
		_, err = io.WriteString(out, "]}}\n")
		return err
	})
}

func writeHAREntry(w io.Writer, e traffic.EvidenceExchange) error {
	headers := func(head string) []map[string]string {
		out := []map[string]string{}
		lines := strings.Split(strings.ReplaceAll(head, "\r\n", "\n"), "\n")
		for _, line := range lines[1:] {
			k, v, ok := strings.Cut(line, ":")
			if ok {
				out = append(out, map[string]string{"name": k, "value": strings.TrimSpace(v)})
			}
		}
		return out
	}
	reqHeaders := headers(e.ReqHead)
	respHeaders := headers(e.RespHead)
	mime := "application/octet-stream"
	for _, h := range reqHeaders {
		if strings.EqualFold(h["name"], "Content-Type") {
			mime = h["value"]
		}
	}
	query := []map[string]string{}
	if u, err := url.Parse(e.URL); err == nil {
		for k, vals := range u.Query() {
			for _, v := range vals {
				query = append(query, map[string]string{"name": k, "value": v})
			}
		}
	}
	proto := ""
	if lines := strings.SplitN(e.ReqHead, "\n", 2); len(lines) > 0 {
		fields := strings.Fields(lines[0])
		if len(fields) > 2 {
			proto = fields[2]
		}
	}
	req := map[string]any{"method": e.Method, "url": e.URL, "httpVersion": proto, "headers": reqHeaders, "cookies": []any{}, "queryString": query, "headersSize": -1, "bodySize": e.ReqLen}
	resp := map[string]any{"status": e.Status, "statusText": "", "httpVersion": "", "headers": respHeaders, "cookies": []any{}, "redirectURL": "", "headersSize": -1, "bodySize": e.RespLen}
	// Write large bodies incrementally into JSON strings; base64 needs no escaping.
	emit := func(v any) error {
		b, err := json.Marshal(v)
		if err != nil {
			return err
		}
		_, err = w.Write(b)
		return err
	}
	write := func(s string) error { _, err := io.WriteString(w, s); return err }
	body := func(r io.Reader, expected int64) error {
		enc := base64.NewEncoder(base64.StdEncoding, w)
		n, err := io.Copy(enc, r)
		closeErr := enc.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		if n != expected {
			return fmt.Errorf("正文长度不匹配: expected=%d actual=%d", expected, n)
		}
		return nil
	}
	if err := write(`{"startedDateTime":`); err != nil {
		return err
	}
	if err := emit(time.Unix(e.TS, 0).UTC().Format(time.RFC3339)); err != nil {
		return err
	}
	if err := write(`,"time":-1,"timings":{"send":-1,"wait":-1,"receive":-1},"_captureId":`); err != nil {
		return err
	}
	if err := emit(e.ID); err != nil {
		return err
	}
	raw, _ := json.Marshal(req)
	if err := write(`,"request":` + strings.TrimSuffix(string(raw), "}") + `,"postData":{"mimeType":`); err != nil {
		return err
	}
	if err := emit(mime); err != nil {
		return err
	}
	if err := write(`,"_encoding":"base64","text":"`); err != nil {
		return err
	}
	if err := body(e.Request, e.ReqLen); err != nil {
		return err
	}
	if err := write(`"}},"response":`); err != nil {
		return err
	}
	raw, _ = json.Marshal(resp)
	if err := write(strings.TrimSuffix(string(raw), "}") + `,"content":{"size":` + fmt.Sprint(e.RespLen) + `,"mimeType":`); err != nil {
		return err
	}
	if err := emit(e.ContentType); err != nil {
		return err
	}
	if err := write(`,"encoding":"base64","text":"`); err != nil {
		return err
	}
	if err := body(e.Response, e.RespLen); err != nil {
		return err
	}
	return write(`"}}}`)
}
