package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Autumn-27/artex/db"
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) queueFindingReport(ctx context.Context, id int64) (job db.FindingReportJob, resultErr error) {
	f, err := s.m.pg.GetFinding(id)
	if err != nil {
		return db.FindingReportJob{}, err
	}
	if f == nil || f.NodeID == nil {
		return db.FindingReportJob{}, errors.New("发现缺少探索节点，无法交给 reporter")
	}
	j, created, err := s.m.pg.QueueFindingReport(ctx, id)
	if err != nil || !created {
		return j, err
	}
	job = j
	defer func() {
		if resultErr != nil {
			_ = s.m.pg.FinishFindingReport(context.Background(), j.ID, resultErr.Error())
		}
	}()
	a, err := s.m.pg.GetAgentByKey("reporter")
	if err != nil {
		return db.FindingReportJob{}, err
	}
	if a == nil || !a.Enabled {
		return db.FindingReportJob{}, errors.New("报告 Agent 未配置或已停用")
	}
	if s.resolveChatAgent(&db.Conversation{AgentKey: "reporter"}) == nil {
		return db.FindingReportJob{}, errors.New(s.chatUnavailableReason())
	}
	for _, key := range []string{"update_finding_report", "get_finding_traffic"} {
		t, e := s.m.pg.GetTool(key)
		if e != nil {
			return db.FindingReportJob{}, e
		}
		if t == nil || !t.Enabled || !containsFold(t.Agents, "reporter") {
			return db.FindingReportJob{}, fmt.Errorf("reporter 未绑定启用的 %s 工具", key)
		}
	}

	task := int64(0)
	if f.TaskID != nil {
		task = *f.TaskID
	}
	msg := fmt.Sprintf("仅根据已有证据生成详细报告，不重新探测目标。finding_id=%d，finding_node_id=%d，证据版本=%d。先调用 get_finding_traffic；如版本不同请停止并说明证据已变更，保留旧报告。最后 update_finding_report(finding_id=%d, evidence_version=%d, report=正文)。", id, *f.NodeID, j.EvidenceVersion, *f.NodeID, j.EvidenceVersion)
	cfg := s.readTriggerBehavior("reporter")
	s.queueMu.Lock()
	s.triggerCfg["reporter"] = cfg
	s.triggerQ["reporter"] = append(s.triggerQ["reporter"], triggeredRun{agentKey: "reporter", reportJobID: j.ID, title: fmt.Sprintf("报告生成 · finding#%d", id), message: msg, taskID: task, mergeable: false})
	s.pumpLocked("reporter")
	s.queueMu.Unlock()
	return j, nil
}
func (s *Server) findingReportStatus(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	before, err := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	if r.URL.Query().Get("before") != "" && (err != nil || before <= 0) {
		writeErr(w, 400, "无效的历史版本游标")
		return
	}
	jobs, versions, err := s.m.pg.FindingReportStatus(r.Context(), id, before)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"jobs": jobs, "versions": versions, "has_more": len(versions) == 20})
}
func (s *Server) regenerateFindingReport(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	j, err := s.queueFindingReport(r.Context(), id)
	if err != nil {
		writeErr(w, 409, err.Error())
		return
	}
	writeJSON(w, 202, j)
}
func findingIDFromOutput(output string) int64 {
	start := strings.Index(output, "{")
	if start < 0 {
		return 0
	}
	var result struct {
		FindingID int64 `json:"finding_id,string"`
	}
	_ = json.NewDecoder(strings.NewReader(output[start:])).Decode(&result)
	return result.FindingID
}
