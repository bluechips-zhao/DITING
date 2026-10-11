package server

import (
	"context"
	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/norma/llm"
	"iter"
	"net/http"
	"strings"
	"time"
)

type executionKey struct{}
type executionContext struct {
	Engine   *Engine
	IntentID int64
}
type WorkerRuntime struct {
	ConstraintsVersion string    `json:"constraints_version,omitempty"`
	IntentID           int64     `json:"intent_id,string"`
	TaskID             string    `json:"task_id"`
	Worker             string    `json:"worker"`
	Phase              string    `json:"phase"`
	Tool               string    `json:"tool,omitempty"`
	Since              time.Time `json:"since"`
	StartedAt          time.Time `json:"started_at"`
	ElapsedSeconds     int64     `json:"elapsed_seconds"`
	ToolCalls          int       `json:"tool_calls"`
	WriteCalls         int       `json:"write_calls"`
	LastToolAt         time.Time `json:"last_tool_at,omitempty"`
	LastWriteAt        time.Time `json:"last_write_at,omitempty"`
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

// The counter covers actual provider calls, including retries, and is released
// when the consumer stops a stream early. Tool execution never holds this count.
type observedTaskProvider struct {
	base   llm.Provider
	engine *Engine
	taskID string
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
		defer func() { p.finish(call, usage) }()
		end := p.begin(ctx)
		defer end()
		for event, err := range p.base.Stream(ctx, request) {
			usage.InputTokens = max(usage.InputTokens, event.Usage.InputTokens)
			usage.OutputTokens = max(usage.OutputTokens, event.Usage.OutputTokens)
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
	message, reason, usage, err := p.base.Complete(ctx, request)
	p.finish(call, usage)
	return message, reason, usage, err
}

func workToolPhase(name string) string {
	if strings.EqualFold(name, "sleep") {
		return "sleep"
	}
	return "tool"
}
