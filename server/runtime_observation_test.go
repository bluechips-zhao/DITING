package server

import (
	"context"
	"github.com/Autumn-27/norma/llm"
	"iter"
	"testing"
)

type runtimeProbe struct{ check func() }

func (p runtimeProbe) Stream(context.Context, llm.CompletionRequest) iter.Seq2[llm.StreamEvent, error] {
	return func(y func(llm.StreamEvent, error) bool) { p.check(); y(llm.StreamEvent{}, nil) }
}
func (p runtimeProbe) Complete(context.Context, llm.CompletionRequest) (llm.Message, string, llm.Usage, error) {
	p.check()
	return llm.Message{}, "", llm.Usage{}, nil
}
func TestRuntimeProviderTracksOnlyActualCall(t *testing.T) {
	e := NewEngine(nil)
	p := observedTaskProvider{engine: e, taskID: "42", base: runtimeProbe{check: func() {
		if e.ActiveLLMCalls("42") != 1 {
			t.Fatal("request not counted")
		}
	}}}
	stream := p.Stream(t.Context(), llm.CompletionRequest{})
	if e.ActiveLLMCalls("42") != 0 {
		t.Fatal("unconsumed stream counted")
	}
	for range stream {
		break
	}
	if e.ActiveLLMCalls("42") != 0 {
		t.Fatal("early stream exit leaked counter")
	}
	_, _, _, _ = p.Complete(t.Context(), llm.CompletionRequest{})
	if e.ActiveLLMCalls("42") != 0 {
		t.Fatal("complete leaked counter")
	}
}
