package server

import (
	"context"
	"github.com/Autumn-27/artex/agent"
	"github.com/Autumn-27/norma/agentcore"
	actool "github.com/Autumn-27/norma/tool"
)

// Report regeneration can read captured evidence and save prose, never probe.
func reportOnlyContext(ctx context.Context) context.Context {
	return agent.WithRunPolicy(ctx, func(opts *agentcore.Options) {
		allowed := map[string]bool{"update_finding_report": true, "get_finding_traffic": true, "get_task_node_detail": true, "list_task_findings": true, "get_task_worker_trace": true, "get_task_worker_traces": true, "search_task_worker_traces": true, "get_task_graph": true}
		filtered := []actool.CoreTool{}
		for _, t := range opts.Tools {
			if allowed[t.Name()] {
				filtered = append(filtered, t)
			}
		}
		opts.Tools = filtered
		opts.DeferredTools = nil
		opts.EnableWebFetch = false
		opts.EnableWebSearch = false
	})
}
