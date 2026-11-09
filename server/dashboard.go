package server

import (
	"net/http"
	"sort"
)

type dashboardTaskTokenGroup struct {
	ProfileID *int64        `json:"llm_profile_id"`
	Date      string        `json:"date"`
	Tasks     int           `json:"tasks"`
	Tokens    TokenTotalDTO `json:"tokens"`
}

type dashboardTaskSummary struct {
	Total        int                       `json:"total"`
	StatusCounts map[string]int            `json:"status_counts"`
	Recent       []TaskDTO                 `json:"recent"`
	TokenGroups  []dashboardTaskTokenGroup `json:"token_groups"`
}

// Preserve all task totals and the legacy creation-date/profile token buckets;
// only the recent table needs descriptions and other full task fields.
func summarizeDashboardTasks(tasks []TaskDTO) dashboardTaskSummary {
	out := dashboardTaskSummary{
		Total: len(tasks), StatusCounts: map[string]int{},
		Recent: append([]TaskDTO{}, tasks...), TokenGroups: []dashboardTaskTokenGroup{},
	}
	type key struct {
		profile    int64
		hasProfile bool
		date       string
	}
	indices := map[key]int{}
	for _, task := range tasks {
		out.StatusCounts[task.Status]++
		date := task.CreatedAt
		if len(date) > 10 {
			date = date[:10]
		}
		groupKey := key{date: date}
		if task.LLMProfileID != nil {
			groupKey.profile, groupKey.hasProfile = *task.LLMProfileID, true
		}
		index, ok := indices[groupKey]
		if !ok {
			index = len(out.TokenGroups)
			indices[groupKey] = index
			out.TokenGroups = append(out.TokenGroups, dashboardTaskTokenGroup{ProfileID: task.LLMProfileID, Date: date})
		}
		group := &out.TokenGroups[index]
		group.Tasks++
		group.Tokens.InputTokens += task.Tokens.InputTokens
		group.Tokens.OutputTokens += task.Tokens.OutputTokens
		group.Tokens.CacheReadTokens += task.Tokens.CacheReadTokens
		group.Tokens.CacheWriteTokens += task.Tokens.CacheWriteTokens
	}
	// LastActivity is always present in the full API, including zero. Stable
	// ordering preserves the manager's pin/ID ordering for equal timestamps.
	sort.SliceStable(out.Recent, func(i, j int) bool { return out.Recent[i].LastActivity > out.Recent[j].LastActivity })
	if len(out.Recent) > 5 {
		out.Recent = out.Recent[:5]
	}
	return out
}

func (s *Server) dashboardTasks(w http.ResponseWriter, r *http.Request) {
	tasks, _, err := s.taskListSnapshot()
	if err != nil {
		writeErr(w, 500, "读取任务统计失败")
		return
	}
	writeJSON(w, 200, summarizeDashboardTasks(tasks))
}
