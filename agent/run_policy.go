package agent

import (
	"context"
	"github.com/Autumn-27/norma/agentcore"
)

type runPolicyKey struct{}

// Host policy applies after tool/guard assembly, covering SDK and deferred tools.
func WithRunPolicy(ctx context.Context, apply func(*agentcore.Options)) context.Context {
	if previous, ok := ctx.Value(runPolicyKey{}).(func(*agentcore.Options)); ok {
		next := apply
		apply = func(opts *agentcore.Options) { previous(opts); next(opts) }
	}
	return context.WithValue(ctx, runPolicyKey{}, apply)
}
func applyRunPolicy(ctx context.Context, opts *agentcore.Options) {
	if fn, ok := ctx.Value(runPolicyKey{}).(func(*agentcore.Options)); ok {
		fn(opts)
	}
}
func newPolicySession(ctx context.Context, opts agentcore.Options) *agentcore.Session {
	applyRunPolicy(ctx, &opts)
	return agentcore.NewSession(opts)
}
