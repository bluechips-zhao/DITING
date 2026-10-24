package server

import (
	"context"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/norma/llm"
	"iter"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type executionKey struct{}
type executionContext struct {
	Engine   *Engine
	IntentID int64
}
type WorkerRuntime struct {
	ConstraintsVersion  string    `json:"constraints_version,omitempty"`
	IntentID            int64     `json:"intent_id,string"`
	TaskID              string    `json:"task_id"`
	Worker              string    `json:"worker"`
	Phase               string    `json:"phase"`
	Tool                string    `json:"tool,omitempty"`
	Since               time.Time `json:"since"`
	StartedAt           time.Time `json:"started_at"`
	ElapsedSeconds      int64     `json:"elapsed_seconds"`
	TotalElapsedSeconds int64     `json:"total_elapsed_seconds"`
	ToolCalls           int       `json:"tool_calls"`
	WriteCalls          int       `json:"write_calls"`
	LastToolAt          time.Time `json:"last_tool_at,omitempty"`
	LastWriteAt         time.Time `json:"last_write_at,omitempty"`
}

func (e *Engine) setWorkPhase(id int64, phase, tool string, completed, success bool) {
	e.workMu.Lock()
	defer e.workMu.Unlock()
	w := e.work[id]
	if w == nil {
		return
	}
	w.runtime.Phase, w.runtime.Tool, w.runtime.Since = phase, tool, time.Now().UTC()
	if completed {
		w.runtime.LastToolAt = time.Now().UTC()
		w.runtime.ToolCalls++
		if success && (tool == "record_fact" || tool == "report_finding" || tool == "insert_assets") {
			w.runtime.WriteCalls++
			w.runtime.LastWriteAt = time.Now().UTC()
		}
	}
}

func (e *Engine) observeWorkActivity(id int64, a db.Activity) {
	if a.Kind == "intercept_request" {
		e.setWorkPhase(id, "approval", a.Tool, false, false)
	}
}

func (e *Engine) workSnapshot(taskID string) []WorkerRuntime {
	e.workMu.Lock()
	defer e.workMu.Unlock()
	out := []WorkerRuntime{}
	for _, w := range e.work {
		if w.runtime.TaskID == taskID {
			v := w.runtime
			v.ElapsedSeconds = int64(time.Since(v.Since).Seconds())
			v.TotalElapsedSeconds = max(0, int64(time.Since(v.StartedAt).Seconds()))
			out = append(out, v)
		}
	}
	return out
}

func (s *Server) taskRuntime(w http.ResponseWriter, r *http.Request) {
	t, ok := s.m.Task(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, "task not found")
		return
	}
	writeJSON(w, 200, map[string]any{"workers": s.engine.workSnapshot(t.ID), "llm_in_flight": s.engine.ActiveLLMCalls(t.ID), "settling": s.engine.isSettling(t.ID), "observed_at": time.Now().UTC()})
}

// The counter covers provider method invocations (SDK-internal HTTP retries
// are not visible here), and is released
// when the consumer stops a stream early. Tool execution never holds this count.
type observedTaskProvider struct {
	base   llm.Provider
	engine *Engine
	taskID string
}

func (p *observedTaskProvider) trackModelCall(ctx context.Context) func(llm.Usage, string, bool) {
	pg := p.engine.budgetPG()
	if pg == nil {
		return func(llm.Usage, string, bool) {}
	}
	taskID, err := strconv.ParseInt(p.taskID, 10, 64)
	if err != nil {
		return func(llm.Usage, string, bool) {}
	}
	x, _ := ctx.Value(executionKey{}).(executionContext)
	id, err := pg.StartModelCall(ctx, taskID, x.IntentID)
	if err != nil {
		log.Printf("[model-calls] could not record call for task %s", p.taskID)
		return func(llm.Usage, string, bool) {}
	}
	started := time.Now()
	return func(usage llm.Usage, status string, reported bool) {
		finishCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := pg.FinishModelCall(finishCtx, id, time.Since(started).Milliseconds(), int64(usage.InputTokens), int64(usage.OutputTokens), status, reported); err != nil {
			log.Printf("[model-calls] could not record outcome for task %s call %d", p.taskID, id)
		}
	}
}

func (s *Server) taskExecutionStats(w http.ResponseWriter, r *http.Request) {
	id, err := parseTaskID(r.PathValue("id"))
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	task, err := s.m.pg.GetTask(id)
	if err != nil {
		writeErr(w, 500, "统计读取失败")
		return
	}
	if task == nil {
		writeErr(w, 404, "task not found")
		return
	}
	stats, err := s.m.pg.DirectionExecutionStats(r.Context(), id)
	if err != nil {
		writeErr(w, 500, "统计读取失败")
		return
	}
	writeJSON(w, 200, map[string]any{"directions": stats, "observed_at": time.Now().UTC(), "coverage": "provider_method_invocations_since_2.6.0"})
}

func (p *observedTaskProvider) begin(ctx context.Context) func() {
	p.engine.BeginLLMCall(p.taskID)
	x, _ := ctx.Value(executionKey{}).(executionContext)
	if x.Engine != nil {
		phase := "model"
		if x.Engine.isSettling(p.taskID) {
			phase = "settling_model"
		}
		x.Engine.setWorkPhase(x.IntentID, phase, "", false, false)
	}
	return func() {
		p.engine.EndLLMCall(p.taskID)
		if x.Engine != nil {
			x.Engine.setWorkPhase(x.IntentID, "planning", "", false, false)
		}
	}
}
func (p *observedTaskProvider) Stream(ctx context.Context, req llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) {
		request, call, err := p.reserve(ctx, req)
		if err != nil {
			yield(llm.StreamEvent{}, err)
			return
		}
		usage := llm.Usage{}
		completed := false
		failed := false
		defer func() { p.finish(call, usage, completed && !failed && ctx.Err() == nil) }()
		end := p.begin(ctx)
		defer end()
		finishRecord := p.trackModelCall(ctx)
		defer func() {
			status := "interrupted"
			if failed {
				status = "failed"
			} else if completed && ctx.Err() == nil {
				status = "completed"
			}
			finishRecord(usage, status, status == "completed" && usage.InputTokens+usage.OutputTokens > 0)
		}()
		for event, err := range p.base.Stream(ctx, request) {
			usage.InputTokens = max(usage.InputTokens, event.Usage.InputTokens)
			usage.OutputTokens = max(usage.OutputTokens, event.Usage.OutputTokens)
			if err != nil {
				failed = true
			} else if event.Type == llm.SEMessageStop {
				completed = true
			}
			if !yield(event, err) {
				return
			}
		}
	}
}
func (p *observedTaskProvider) Complete(ctx context.Context, req llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	request, call, err := p.reserve(ctx, req)
	if err != nil {
		return llm.Message{}, "", llm.Usage{}, err
	}
	end := p.begin(ctx)
	defer end()
	finishRecord := p.trackModelCall(ctx)
	status := "interrupted"
	usage := llm.Usage{}
	defer func() { p.finish(call, usage, status == "completed") }()
	defer func() { finishRecord(usage, status, status == "completed" && usage.InputTokens+usage.OutputTokens > 0) }()
	message, reason, usage, err := p.base.Complete(ctx, request)
	if err != nil {
		status = "failed"
	} else {
		status = "completed"
	}
	return message, reason, usage, err
}

func workToolPhase(name string) string {
	if strings.EqualFold(name, "sleep") {
		return "sleep"
	}
	return "tool"
}
