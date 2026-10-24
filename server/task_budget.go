package server

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/norma/agentcore"
	"github.com/Autumn-27/norma/harness"
	"github.com/Autumn-27/norma/llm"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *Server) budgetContext(ctx context.Context, taskID string, chat bool) context.Context {
	id, err := strconv.ParseInt(taskID, 10, 64)
	if err != nil || id <= 0 {
		return ctx
	}
	return agent.WithRunPolicy(ctx, func(opts *agentcore.Options) {
		opts.Hooks = taskBudgetHooks{s: s, id: id, inner: opts.Hooks}
		if chat {
			opts.Provider = &observedTaskProvider{base: opts.Provider, engine: s.engine, taskID: taskID}
		}
	})
}

type taskBudgetHooks struct {
	s     *Server
	id    int64
	inner harness.HookRunner
}

func (h taskBudgetHooks) PreToolUse(ctx context.Context, name string, input []byte) (bool, string, []byte) {
	var replacement []byte
	if h.inner != nil {
		block, message, updated := h.inner.PreToolUse(ctx, name, input)
		if block {
			return block, message, updated
		}
		replacement = updated
	}
	b, err := h.s.m.pg.ConsumeTaskTool(ctx, h.id, settlementTool(name))
	if b.State != "active" {
		h.s.requestBudgetSettle(h.id)
	}
	if err != nil {
		return true, err.Error(), nil
	}
	return false, "", replacement
}
func (h taskBudgetHooks) PostToolUse(ctx context.Context, name string, input, result []byte, isErr bool) {
	if h.inner != nil {
		h.inner.PostToolUse(ctx, name, input, result, isErr)
	}
}
func (h taskBudgetHooks) Stop(ctx context.Context, msg []llm.Message) (bool, []string, string) {
	b, err := h.s.m.pg.TaskBudget(ctx, h.id)
	if err != nil || b.State != "active" {
		return false, nil, ""
	}
	if h.inner != nil {
		return h.inner.Stop(ctx, msg)
	}
	return false, nil, ""
}
func settlementTool(name string) bool {
	switch strings.ToLower(name) {
	case "record_fact", "report_finding", "update_finding_report", "get_finding_traffic", "get_task_node_detail", "get_task_worker_trace", "get_task_worker_traces", "search_task_worker_traces", "list_task_findings", "taskoutput", "task_output", "sleep", "todowrite", "read", "get_task_graph", "prove_goal", "goal_met":
		return true
	}
	return false
}

func (e *Engine) budgetPG() *db.DB {
	if e.m == nil {
		return nil
	}
	return e.m.pg
}
func (p *observedTaskProvider) reserve(ctx context.Context, req llm.CompletionRequest) (llm.CompletionRequest, int64, error) {
	pg := p.engine.budgetPG()
	if pg == nil {
		return req, 0, nil
	}
	id, err := strconv.ParseInt(p.taskID, 10, 64)
	if err != nil {
		return req, 0, nil
	}
	b, err := pg.TaskBudget(ctx, id)
	if err != nil {
		return req, 0, err
	}
	if b.TokenLimit == 0 && b.ToolLimit == 0 {
		return req, 0, nil
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return req, 0, err
	}
	output := req.MaxTokens
	if output <= 0 || output > 8192 {
		output = 8192
	}
	// Reservations estimate input conservatively; they are not provider billing.
	estimate := int64(len(raw))*2 + 4096 + int64(output)
	if b.TokenLimit > 0 {
		remaining := b.TokenLimit - b.TokensUsed - b.TokensReserved
		input := int64(len(raw))*2 + 4096
		if remaining > input && int64(output) > remaining-input {
			output = int(remaining - input)
			estimate = input + int64(output)
		}
	}
	req.MaxTokens = output
	call, b, err := pg.ReserveTaskTokens(ctx, id, estimate)
	if b.State != "active" {
		if p.engine.budgetSettle != nil {
			p.engine.budgetSettle(id)
		}
		req.System = append(append([]string{}, req.System...), "任务累计预算已进入收尾预留。停止新增探测、派发和子任务，只收割已启动后台结果、保存已有证据及阻塞原因。不要声称覆盖完整。")
	}
	return req, call, err
}
func (p *observedTaskProvider) finish(call int64, usage llm.Usage, completed bool) {
	if pg := p.engine.budgetPG(); pg != nil && call > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		used := int64(usage.InputTokens) + int64(usage.OutputTokens)
		if err := pg.FinishTaskTokens(ctx, call, used, completed && used > 0); err != nil {
			if t, ok := p.engine.m.Task(p.taskID); ok {
				p.engine.emitActivity(t, db.Activity{Worker: "system", Kind: "text", IsError: true, Summary: "预算用量结算失败，预留仍保留: " + err.Error()})
			}
		}
	}
}

func (s *Server) requestBudgetSettle(id int64) {
	t, ok := s.m.Task(fmt.Sprint(id))
	if !ok || s.engine.IsDeleting(t.ID) || isTerminalStatus(s.m.TaskStatus(t.ID)) {
		return
	}
	if !s.engine.markSettling(t.ID) {
		return
	}
	s.engine.emitActivity(t, db.Activity{Worker: "system", Kind: "text", Summary: "累计预算进入收尾预留，停止新增工作；额度耗尽与任务失败分开记录"})
	for _, w := range s.engine.workSnapshot(t.ID) {
		_ = s.engine.SteerWork(w.IntentID, "累计预算进入收尾，请收割后台结果、保存已有证据及未完成原因后结束。")
	}
	go func() {
		ctx, cancel := context.WithTimeout(s.ctx, 90*time.Second)
		defer cancel()
		for ctx.Err() == nil {
			busy, err := s.taskBudgetBusy(ctx, id)
			if err == nil && !busy {
				break
			}
			if sleepCtx(ctx, 500*time.Millisecond) {
				break
			}
		}
		if !isTerminalStatus(s.m.TaskStatus(t.ID)) && !s.engine.IsDeleting(t.ID) {
			if err := s.m.ApplyTaskPause(t.ID); err == nil {
				s.engine.Pause(t.ID, agent.AbortPausedByUser)
			}
		}
	}()
}

func (s *Server) taskBudgetBusy(ctx context.Context, id int64) (bool, error) {
	taskID := fmt.Sprint(id)
	if s.engine.inflightCount(taskID) > 0 || s.engine.ActiveLLMCalls(taskID) > 0 || len(s.engine.workSnapshot(taskID)) > 0 {
		return true, nil
	}
	b, err := s.m.pg.TaskBudget(ctx, id)
	if err != nil {
		return true, err
	}
	if b.TokensReserved > 0 {
		return true, nil
	}
	rows, err := s.m.pg.QueryContext(ctx, `SELECT id FROM conversations WHERE source_task_id=$1`, id)
	if err != nil {
		return true, err
	}
	defer rows.Close()
	keys := []string{taskID}
	for rows.Next() {
		var conv int64
		if err = rows.Scan(&conv); err != nil {
			return true, err
		}
		keys = append(keys, s.convBusyKey(conv))
	}
	if err = rows.Err(); err != nil {
		return true, err
	}
	s.chatMu.Lock()
	defer s.chatMu.Unlock()
	for _, k := range keys {
		if s.chatBusy[k] {
			return true, nil
		}
	}
	return false, nil
}
func (s *Server) getTaskBudget(w http.ResponseWriter, r *http.Request) {
	id, err := parseTaskID(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	b, err := s.m.pg.TaskBudget(r.Context(), id)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	writeJSON(w, 200, b)
}
func (s *Server) setTaskBudget(w http.ResponseWriter, r *http.Request) {
	id, err := parseTaskID(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	busy, err := s.taskBudgetBusy(r.Context(), id)
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	if busy {
		writeErr(w, 409, "请先暂停任务并等待当前调用结束，再调整累计预算")
		return
	}
	var b db.TaskBudget
	if !decodeConversationRequest(w, r, &b) {
		return
	}
	b.TaskID = id
	if err = s.m.pg.SetTaskBudget(r.Context(), b); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	s.getTaskBudget(w, r)
}
