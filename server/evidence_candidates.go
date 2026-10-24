package server

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Signals only identify output worth reviewing. They do not prove a vulnerability.
var candidateSignals = []struct {
	id, reason string
	pattern    *regexp.Regexp
}{
	{"nmap-state-v1", "扫描输出包含 VULNERABLE 状态，需要核对目标与可复现证据", regexp.MustCompile(`(?im)^\s*(?:\|[_ ]*)?\s*State:\s*VULNERABLE\s*$`)},
	{"sqlmap-point-v1", "工具输出列出了注入点，需要核对请求与对照结果", regexp.MustCompile(`(?im)^sqlmap identified the following injection point\(s\)`)},
	{"nuclei-match-v1", "工具输出包含扫描模板命中，需要排除误报", regexp.MustCompile(`(?im)^\[[a-z0-9_-]+\]\s+\[(?:http|dns|tcp|ssl|file|javascript)\]\s+\[(?:critical|high|medium|low)\]\s+\S+`)},
}

func (e *Engine) collectEvidenceCandidates(t *Task, intentID int64, unreviewed bool) error {
	if e.m == nil || e.m.pg == nil {
		return nil
	}
	taskID, err := strconv.ParseInt(t.ID, 10, 64)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Page all persisted outputs. A model's final prose is not a source here.
	var cursor int64
	for {
		rows, err := e.m.pg.QueryContext(ctx, `SELECT a.id,COALESCE(a.detail,'') FROM activity a JOIN tasks t ON t.exploration_id=a.exploration_id WHERE t.id=$1 AND a.node_id=$2 AND a.kind='tool_result' AND NOT a.is_error AND a.id>$3 ORDER BY a.id LIMIT 100`, taskID, intentID, cursor)
		if err != nil {
			return err
		}
		type source struct {
			id     int64
			output string
		}
		batch := []source{}
		for rows.Next() {
			var s source
			if err = rows.Scan(&s.id, &s.output); err != nil {
				rows.Close()
				return err
			}
			batch = append(batch, s)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}
		for _, s := range batch {
			cursor = s.id
			matched := false
			for _, signal := range candidateSignals {
				if signal.pattern.MatchString(s.output) {
					matched = true
					if err = e.m.pg.AddEvidenceCandidate(ctx, taskID, intentID, s.id, signal.id, signal.reason); err != nil {
						return err
					}
				}
			}
			if unreviewed && !matched && strings.TrimSpace(s.output) != "" {
				if err = e.m.pg.AddEvidenceCandidate(ctx, taskID, intentID, s.id, "zero-writeback-v1", "该方向结束时没有事实写回；此工具结果尚需核对，不代表漏洞或覆盖完成"); err != nil {
					return err
				}
			}
		}
	}
}

func (s *Server) listEvidenceCandidates(w http.ResponseWriter, r *http.Request) {
	id, err := parseTaskID(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	task, err := s.m.pg.GetTask(id)
	if err != nil {
		writeErr(w, 500, "任务读取失败")
		return
	}
	if task == nil {
		writeErr(w, 404, "task not found")
		return
	}
	before := int64(0)
	if v := r.URL.Query().Get("before"); v != "" {
		before, err = strconv.ParseInt(v, 10, 64)
		if err != nil || before < 0 {
			writeErr(w, 400, "invalid before cursor")
			return
		}
	}
	rows, err := s.m.pg.ListEvidenceCandidates(r.Context(), id, before, 51)
	if err != nil {
		writeErr(w, 500, "候选读取失败")
		return
	}
	more := len(rows) > 50
	if more {
		rows = rows[:50]
	}
	writeJSON(w, 200, map[string]any{"candidates": rows, "has_more": more})
}

func (s *Server) reviewEvidenceCandidate(w http.ResponseWriter, r *http.Request) {
	taskID, err := parseTaskID(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	id, err := parseTaskID(r.PathValue("candidate"))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	var body struct {
		State string `json:"state"`
	}
	if !decodeConversationRequest(w, r, &body) {
		return
	}
	if body.State != "reviewed" && body.State != "dismissed" {
		writeErr(w, 400, "只能标记已核对或忽略；候选不自动登记为漏洞")
		return
	}
	changed, err := s.m.pg.ReviewEvidenceCandidate(r.Context(), taskID, id, body.State)
	if err != nil {
		writeErr(w, 500, "候选更新失败")
		return
	}
	if !changed {
		writeErr(w, 409, "候选不存在或已处理，请刷新")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (s *Server) evidenceCandidateSource(w http.ResponseWriter, r *http.Request) {
	taskID, err := parseTaskID(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	id, err := parseTaskID(r.PathValue("candidate"))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	output, err := s.m.pg.CandidateSource(r.Context(), taskID, id)
	if errors.Is(err, sql.ErrNoRows) {
		writeErr(w, 404, "candidate not found")
		return
	}
	if err != nil {
		writeErr(w, 500, "来源读取失败")
		return
	}
	runes := []rune(output)
	truncated := len(runes) > 32768
	if truncated {
		output = string(runes[:32768])
	}
	writeJSON(w, 200, map[string]any{"output": output, "truncated": truncated})
}
