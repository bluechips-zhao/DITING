package server

import (
	"context"
	"errors"
	"iter"
	"strconv"
	"testing"

	"github.com/Autumn-27/artex/db"
	"github.com/Autumn-27/norma/llm"
)

type budgetUsageProvider struct {
	events []llm.StreamEvent
	err    error
	usage  llm.Usage
}

func (p budgetUsageProvider) Stream(context.Context, llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(yield func(llm.StreamEvent, error) bool) {
		for _, event := range p.events {
			if !yield(event, nil) {
				return
			}
		}
		if p.err != nil {
			yield(llm.StreamEvent{}, p.err)
		}
	}
}

func (p budgetUsageProvider) Complete(context.Context, llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	return llm.Message{}, "end_turn", p.usage, p.err
}

func TestObservedBudgetRejectsPartialUsageAsFinal(t *testing.T) {
	m := newIsolatedTestManager(t)
	e := NewEngine(m)
	partial := llm.StreamEvent{Type: llm.SEMessageStart, Usage: llm.Usage{InputTokens: 20}}
	final := llm.StreamEvent{Type: llm.SEMessageDelta, StopReason: "end_turn", Usage: llm.Usage{OutputTokens: 11}}
	stop := llm.StreamEvent{Type: llm.SEMessageStop}
	for _, tc := range []struct {
		name     string
		provider budgetUsageProvider
		early    bool
		complete bool
		known    bool
	}{
		{"finished_stream", budgetUsageProvider{events: []llm.StreamEvent{partial, final, stop}}, false, false, true},
		{"partial_error", budgetUsageProvider{events: []llm.StreamEvent{partial}, err: errors.New("connection lost")}, false, false, false},
		{"partial_eof", budgetUsageProvider{events: []llm.StreamEvent{partial}}, false, false, false},
		{"consumer_stopped", budgetUsageProvider{events: []llm.StreamEvent{partial, final, stop}}, true, false, false},
		{"missing_usage", budgetUsageProvider{events: []llm.StreamEvent{stop}}, false, false, false},
		{"finished_complete", budgetUsageProvider{usage: llm.Usage{InputTokens: 20, OutputTokens: 11}}, false, true, true},
		{"errored_complete", budgetUsageProvider{usage: llm.Usage{InputTokens: 20, OutputTokens: 11}, err: context.DeadlineExceeded}, false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			task, err := m.pg.CreateTask("budget usage regression", "local", nil, 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			if err = m.pg.SetTaskBudget(t.Context(), db.TaskBudget{TaskID: task.ID, TokenLimit: 100000}); err != nil {
				t.Fatal(err)
			}
			id := strconv.FormatInt(task.ID, 10)
			p := observedTaskProvider{engine: e, taskID: id, base: tc.provider}
			if tc.complete {
				_, _, _, _ = p.Complete(t.Context(), llm.CompletionRequest{MaxTokens: 64})
			} else {
				for range p.Stream(t.Context(), llm.CompletionRequest{MaxTokens: 64}) {
					if tc.early {
						break
					}
				}
			}
			var reserved, charged int64
			var state string
			if err = m.pg.QueryRow(`SELECT reserved,charged,state FROM task_budget_calls WHERE task_id=$1`, task.ID).Scan(&reserved, &charged, &state); err != nil {
				t.Fatal(err)
			}
			if tc.known {
				if state != "reconciled" || charged != 31 {
					t.Fatalf("known usage: state=%s charged=%d", state, charged)
				}
			} else if state != "unknown" || charged != reserved {
				t.Fatalf("partial usage released reservation: state=%s charged=%d reserved=%d", state, charged, reserved)
			}
			b, err := m.pg.TaskBudget(t.Context(), task.ID)
			if err != nil || b.TokensReserved != 0 || b.TokensUsed != charged {
				t.Fatalf("budget=%+v err=%v", b, err)
			}
			if (b.UnreportedRequests == 0) != tc.known || e.ActiveLLMCalls(id) != 0 {
				t.Fatalf("accounting/counter leak: budget=%+v", b)
			}
			stats, err := m.pg.DirectionExecutionStats(t.Context(), task.ID)
			if err != nil || len(stats) != 1 || stats[0].ModelCalls != 1 || stats[0].ModelRunning != 0 || (stats[0].UnknownUsageCalls == 0) != tc.known {
				t.Fatalf("persistent provider tracking: %+v %v", stats, err)
			}
		})
	}
}
