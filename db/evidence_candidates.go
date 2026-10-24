package db

import (
	"context"
	"errors"
	"time"
)

type EvidenceCandidate struct {
	ID         int64      `json:"id,string"`
	TaskID     int64      `json:"task_id,string"`
	IntentID   int64      `json:"intent_id,string"`
	ActivityID int64      `json:"activity_id,string"`
	Detector   string     `json:"detector"`
	Reason     string     `json:"reason"`
	State      string     `json:"state"`
	Tool       string     `json:"tool"`
	ToolUseID  string     `json:"tool_use_id"`
	CreatedAt  time.Time  `json:"created_at"`
	ReviewedAt *time.Time `json:"reviewed_at"`
}

func (d *DB) AddEvidenceCandidate(ctx context.Context, taskID, intentID, activityID int64, detector, reason string) error {
	if detector == "" || len(detector) > 80 || reason == "" || len(reason) > 512 {
		return errors.New("invalid evidence candidate metadata")
	}
	res, err := d.ExecContext(ctx, `INSERT INTO task_evidence_candidates(task_id,intent_id,activity_id,detector,reason)
	 SELECT t.id,n.id,a.id,$4,$5 FROM tasks t JOIN exploration_nodes n ON n.exploration_id=t.exploration_id AND n.id=$2 AND n.kind='intent'
	 JOIN activity a ON a.exploration_id=t.exploration_id AND a.node_id=n.id AND a.id=$3 AND a.kind='tool_result' AND NOT a.is_error
	 WHERE t.id=$1 ON CONFLICT(activity_id,detector) DO NOTHING`, taskID, intentID, activityID, detector, reason)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		var valid bool
		err = d.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM task_evidence_candidates WHERE task_id=$1 AND intent_id=$2 AND activity_id=$3 AND detector=$4)`, taskID, intentID, activityID, detector).Scan(&valid)
		if err != nil {
			return err
		}
		if !valid {
			return errors.New("candidate source is not a successful tool result in this task direction")
		}
	}
	return nil
}

func (d *DB) ListEvidenceCandidates(ctx context.Context, taskID, before int64, limit int) ([]EvidenceCandidate, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := d.QueryContext(ctx, `SELECT c.id,c.task_id,c.intent_id,c.activity_id,c.detector,c.reason,c.state,COALESCE(a.tool,''),COALESCE(a.tool_use_id,''),c.created_at,c.reviewed_at
	 FROM task_evidence_candidates c JOIN activity a ON a.id=c.activity_id WHERE c.task_id=$1 AND ($2=0 OR c.id<$2) ORDER BY c.id DESC LIMIT $3`, taskID, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EvidenceCandidate{}
	for rows.Next() {
		var c EvidenceCandidate
		if err = rows.Scan(&c.ID, &c.TaskID, &c.IntentID, &c.ActivityID, &c.Detector, &c.Reason, &c.State, &c.Tool, &c.ToolUseID, &c.CreatedAt, &c.ReviewedAt); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (d *DB) ReviewEvidenceCandidate(ctx context.Context, taskID, id int64, state string) (bool, error) {
	if state != "reviewed" && state != "dismissed" {
		return false, errors.New("state must be reviewed or dismissed")
	}
	res, err := d.ExecContext(ctx, `UPDATE task_evidence_candidates SET state=$3,reviewed_at=NOW() WHERE task_id=$1 AND id=$2 AND state='pending'`, taskID, id, state)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (d *DB) CandidateSource(ctx context.Context, taskID, id int64) (string, error) {
	var output string
	err := d.QueryRowContext(ctx, `SELECT COALESCE(a.detail,'') FROM task_evidence_candidates c JOIN activity a ON a.id=c.activity_id AND a.node_id=c.intent_id JOIN tasks t ON t.id=c.task_id AND t.exploration_id=a.exploration_id WHERE c.task_id=$1 AND c.id=$2`, taskID, id).Scan(&output)
	return output, err
}
