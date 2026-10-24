package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// RecoverInterruptedIntercepts runs before this server starts accepting work.
// Pending calls belong to the previous process and need a new approval on replay.
// An allowed call without a result may already have executed; do not call it a
// failure, success, or safe operation to retry.
func (d *DB) RecoverInterruptedIntercepts(ctx context.Context) (int64, error) {
	result, err := d.ExecContext(ctx, `UPDATE intercept_pending SET
		status=CASE WHEN status='pending' THEN 'denied' ELSE status END,
		decided_at=CASE WHEN status='pending' THEN NOW() ELSE decided_at END,
		reason=CASE WHEN status='pending' THEN reason || '（服务已中断，本次审批取消；恢复操作需重新审批）' ELSE reason END,
		audit=CASE WHEN audit IS NULL THEN NULL ELSE audit ||
		CASE WHEN status='pending' THEN jsonb_build_object(
			'effective_action','deny','decision_reason','服务已中断，本次审批取消；恢复操作需重新审批',
			'execution_status','not_executed','execution_ended_at',NOW())
		ELSE jsonb_build_object('execution_status','unknown',
			'output','服务已中断，未取得执行结果；操作可能已经执行，重新尝试前需核对实际状态。',
			'output_truncated',false,'execution_ended_at',NOW()) END END
		WHERE status='pending' OR audit->>'execution_status'='awaiting_result'`)
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

// InterceptContextEntry is a bounded, recorded session event, not model reasoning.
type InterceptContextEntry struct {
	Kind      string `json:"kind"`
	Tool      string `json:"tool,omitempty"`
	ToolUseID string `json:"tool_use_id,omitempty"`
	Text      string `json:"text"`
	IsError   bool   `json:"is_error,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
}

// InterceptAudit is captured at review time. It is deliberately excluded from
// polling/list responses; old rows have no audit instead of reconstructed data.
type InterceptAudit struct {
	RunID            string                  `json:"run_id,omitempty"`
	ToolUseID        string                  `json:"tool_use_id,omitempty"`
	Correlation      string                  `json:"correlation"` // exact | ambiguous | unavailable
	InputDigest      string                  `json:"input_digest"`
	UserMessage      string                  `json:"user_message"`
	UserTruncated    bool                    `json:"user_truncated,omitempty"`
	Context          []InterceptContextEntry `json:"context"`
	ContextTruncated bool                    `json:"context_truncated,omitempty"`
	CapturedAt       time.Time               `json:"captured_at"`
	ModelFallback    bool                    `json:"model_fallback,omitempty"`
	ModelInput       json.RawMessage         `json:"model_input,omitempty"`
	ModelInputDigest string                  `json:"model_input_digest,omitempty"`
	InitialAction    string                  `json:"initial_action"`
	InitialReason    string                  `json:"initial_reason"`
	EffectiveAction  string                  `json:"effective_action,omitempty"`
	DecisionReason   string                  `json:"decision_reason,omitempty"`
	RuleName         string                  `json:"rule_name,omitempty"`
	ConfigDigest     string                  `json:"config_digest,omitempty"`
	ProfileID        int64                   `json:"profile_id,omitempty"`
	ExecutionStatus  string                  `json:"execution_status"`
	Output           string                  `json:"output,omitempty"`
	OutputTruncated  bool                    `json:"output_truncated,omitempty"`
	ExecutionEndedAt *time.Time              `json:"execution_ended_at,omitempty"`
}

type InterceptDetail struct {
	InterceptApprovalRow
	Audit *InterceptAudit `json:"audit"`
}

func (d *DB) GetInterceptDetail(id int64) (*InterceptDetail, error) {
	var out InterceptDetail
	var raw []byte
	err := d.QueryRow(approvalRowSelectWithAudit+` WHERE ip.id=$1`, id).Scan(
		&out.ID, &out.RuleID, &out.ConversationID, &out.TaskID, &out.AgentName,
		&out.ToolName, &out.ToolInput, &out.Status, &out.Reason, &out.DecidedAt, &out.CreatedAt,
		&out.DecisionSource, &out.ConvTitle, &out.ConvAgentKey, &out.RuleName, &raw,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &out.Audit); err != nil {
			return nil, err
		}
	}
	return &out, nil
}

// ResolveIntercept atomically settles a pending request. A timeout cannot
// overwrite a human decision and repeat decisions cannot rewrite history.
func (d *DB) ResolveIntercept(id int64, status, action, reason string) (bool, error) {
	execution := "not_executed"
	if action == "allow" {
		execution = "awaiting_result"
	}
	patch, err := json.Marshal(map[string]any{
		"effective_action": action, "decision_reason": reason, "execution_status": execution,
	})
	if err != nil {
		return false, err
	}
	r, err := d.Exec(`UPDATE intercept_pending SET status=$2, decided_at=NOW(),
		audit=CASE WHEN audit IS NULL THEN NULL ELSE audit || $3::jsonb ||
        CASE WHEN $4='allow' AND audit->>'correlation' IS DISTINCT FROM 'exact'
        THEN '{"execution_status":"unknown"}'::jsonb ELSE '{}'::jsonb END END
        WHERE id=$1 AND status='pending'`, id, status, patch, action)
	if err != nil {
		return false, err
	}
	n, err := r.RowsAffected()
	return n == 1, err
}

// CompleteIntercept only updates the exact recorded call after it was allowed.
// A blocked tool_result must never be presented as a failed execution.
func (d *DB) CompleteIntercept(id int64, runID, toolUseID, status, output string, truncated bool) error {
	patch, err := json.Marshal(map[string]any{
		"execution_status": status, "output": output, "output_truncated": truncated,
		"execution_ended_at": time.Now().UTC(),
	})
	if err != nil {
		return err
	}
	_, err = d.Exec(`UPDATE intercept_pending SET audit=audit || $4::jsonb
		WHERE id=$1 AND audit->>'run_id'=$2 AND audit->>'tool_use_id'=$3
		AND audit->>'effective_action'='allow' AND audit->>'execution_status'='awaiting_result'`,
		id, runID, toolUseID, patch)
	return err
}
