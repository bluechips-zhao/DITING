package db

import (
	"context"
	"errors"
)

// ModelCall records a provider method invocation, not its internal HTTP retries.
// Requests, responses, credentials and provider error bodies are never stored here.
func (d *DB) StartModelCall(ctx context.Context, taskID, intentID int64) (int64, error) {
	var id int64
	err := d.QueryRowContext(ctx, `INSERT INTO task_model_calls(task_id,intent_id)
		SELECT t.id,CASE WHEN $2=0 THEN NULL ELSE $2 END FROM tasks t WHERE t.id=$1
		AND ($2=0 OR EXISTS(SELECT 1 FROM exploration_nodes n WHERE n.id=$2 AND n.kind='intent' AND n.exploration_id=t.exploration_id))
		RETURNING id`, taskID, intentID).Scan(&id)
	return id, err
}

func (d *DB) FinishModelCall(ctx context.Context, id, elapsedMS, input, output int64, status string, reported bool) error {
	if elapsedMS < 0 || input < 0 || output < 0 || (status != "completed" && status != "failed" && status != "interrupted") {
		return errors.New("invalid model call outcome")
	}
	_, err := d.ExecContext(ctx, `UPDATE task_model_calls SET finished_at=NOW(),elapsed_ms=$2,elapsed_recorded=true,input_tokens=$3,output_tokens=$4,status=$5,usage_reported=$6 WHERE id=$1 AND status='running'`, id, elapsedMS, input, output, status, reported)
	return err
}

func (d *DB) RecoverInterruptedModelCalls(ctx context.Context) error {
	_, err := d.ExecContext(ctx, `UPDATE task_model_calls SET status='interrupted',finished_at=NOW(),usage_reported=false WHERE status='running'`)
	return err
}

type DirectionExecutionStats struct {
	IntentID            int64  `json:"intent_id,string"`
	Summary             string `json:"summary"`
	State               string `json:"state"`
	ModelCalls          int64  `json:"model_calls"`
	ModelRunning        int64  `json:"model_running"`
	ModelFailed         int64  `json:"model_failed"`
	ModelInterrupted    int64  `json:"model_interrupted"`
	ModelElapsedMS      int64  `json:"model_elapsed_ms"`
	UnknownElapsedCalls int64  `json:"unknown_elapsed_calls"`
	InputTokens         int64  `json:"input_tokens"`
	OutputTokens        int64  `json:"output_tokens"`
	UnknownUsageCalls   int64  `json:"unknown_usage_calls"`
	ToolResults         int64  `json:"tool_results"`
	ToolErrors          int64  `json:"tool_errors"`
	ConfirmedFacts      int64  `json:"confirmed_facts"`
	ConfirmedFindings   int64  `json:"confirmed_findings"`
	CandidateFindings   int64  `json:"candidate_findings"`
}

func (d *DB) DirectionExecutionStats(ctx context.Context, taskID int64) ([]DirectionExecutionStats, error) {
	rows, err := d.QueryContext(ctx, `WITH own AS (SELECT exploration_id FROM tasks WHERE id=$1),
	 directions AS (SELECT n.id,COALESCE(n.payload->>'summary','') AS summary,n.state FROM exploration_nodes n,own WHERE n.exploration_id=own.exploration_id AND n.kind='intent'
	 UNION ALL SELECT 0,'规划与未归属方向的调用','unassigned' WHERE EXISTS(SELECT 1 FROM task_model_calls WHERE task_id=$1 AND intent_id IS NULL)),
	 calls AS (SELECT COALESCE(intent_id,0) AS id,COUNT(*) AS count,
	 COUNT(*) FILTER(WHERE status='running') AS running,COUNT(*) FILTER(WHERE status='failed') AS failed,
	 COUNT(*) FILTER(WHERE status='interrupted') AS interrupted,COALESCE(SUM(elapsed_ms),0) AS elapsed,
	 COUNT(*) FILTER(WHERE status!='running' AND NOT elapsed_recorded) AS unknown_elapsed,
	 COALESCE(SUM(input_tokens),0) AS input,COALESCE(SUM(output_tokens),0) AS output,
	 COUNT(*) FILTER(WHERE status!='running' AND NOT usage_reported) AS unknown FROM task_model_calls WHERE task_id=$1 GROUP BY intent_id),
	 tools AS (SELECT COALESCE(node_id,0) AS id,COUNT(*) AS count,COUNT(*) FILTER(WHERE is_error) AS errors FROM activity,own WHERE activity.exploration_id=own.exploration_id AND kind='tool_result' GROUP BY node_id),
	 writes AS (SELECT e.src_id AS id,COUNT(DISTINCT n.id) FILTER(WHERE n.kind='fact' AND n.state='confirmed') AS facts,
	 COUNT(DISTINCT n.id) FILTER(WHERE n.kind='finding' AND n.state='confirmed') AS findings,
	 COUNT(DISTINCT n.id) FILTER(WHERE n.kind='finding' AND n.state='candidate') AS candidates
	 FROM exploration_edges e JOIN exploration_nodes n ON n.id=e.dst_id,own WHERE e.exploration_id=own.exploration_id AND n.exploration_id=own.exploration_id AND e.rel='yields' GROUP BY e.src_id)
	 SELECT directions.id,summary,state,COALESCE(calls.count,0),COALESCE(running,0),COALESCE(failed,0),COALESCE(interrupted,0),COALESCE(elapsed,0),COALESCE(unknown_elapsed,0),COALESCE(input,0),COALESCE(output,0),COALESCE(unknown,0),COALESCE(tools.count,0),COALESCE(errors,0),COALESCE(facts,0),COALESCE(findings,0),COALESCE(candidates,0)
	 FROM directions LEFT JOIN calls USING(id) LEFT JOIN tools USING(id) LEFT JOIN writes USING(id) ORDER BY directions.id`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DirectionExecutionStats{}
	for rows.Next() {
		var v DirectionExecutionStats
		if err = rows.Scan(&v.IntentID, &v.Summary, &v.State, &v.ModelCalls, &v.ModelRunning, &v.ModelFailed, &v.ModelInterrupted, &v.ModelElapsedMS, &v.UnknownElapsedCalls, &v.InputTokens, &v.OutputTokens, &v.UnknownUsageCalls, &v.ToolResults, &v.ToolErrors, &v.ConfirmedFacts, &v.ConfirmedFindings, &v.CandidateFindings); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
