package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
)

// GoalProof is persisted with the goal so the reason survives tool transcripts.
type GoalProof struct {
	EvidenceID string    `json:"evidence_id"`
	Reason     string    `json:"reason"`
	At         time.Time `json:"at"`
}

// ProveGoal commits the evidence edge, proof and state together. The completion
// check uses COUNT rather than a truncated list. Source evidence is read-only.
func (s *ExplorationStore) ProveGoal(ctx context.Context, goalID, evidenceID int64, reason string) (allMet bool, total int, err error) {
	reason = strings.TrimSpace(reason)
	if goalID <= 0 || evidenceID <= 0 || reason == "" {
		return false, 0, errors.New("goal_id、evidence_id 和达成理由必填")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, 0, err
	}
	defer tx.Rollback()
	var expID int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM explorations WHERE id=$1 FOR UPDATE`, s.expID).Scan(&expID); err != nil {
		return
	}
	var payload []byte
	if err = tx.QueryRowContext(ctx, `SELECT payload FROM exploration_nodes WHERE id=$1 AND exploration_id=$2 AND kind='goal' FOR UPDATE`, goalID, s.expID).Scan(&payload); err != nil {
		if err == sql.ErrNoRows {
			err = errors.New("goal_id 必须是本任务的目标")
		}
		return
	}
	var kind string
	err = tx.QueryRowContext(ctx, `SELECT n.kind FROM exploration_nodes n WHERE n.id=$1 AND n.kind IN ('fact','finding') AND n.state='confirmed' AND NOT EXISTS(SELECT 1 FROM findings f WHERE f.node_id=n.id AND f.status IN ('false_positive','duplicate','ignored')) AND (
	 n.exploration_id=$2 OR EXISTS(SELECT 1 FROM tasks owner JOIN task_relations r ON r.task_id=owner.id
	 JOIN tasks source ON source.id=r.source_task_id AND source.deleted_at IS NULL
	 WHERE owner.exploration_id=$2 AND owner.deleted_at IS NULL AND source.exploration_id=n.exploration_id)) FOR SHARE OF n`, evidenceID, s.expID).Scan(&kind)
	if err != nil {
		if err == sql.ErrNoRows {
			err = errors.New("证据必须是本任务或直接关联任务的事实/漏洞")
		}
		return
	}
	var p map[string]any
	if err = json.Unmarshal(payload, &p); err != nil {
		return
	}
	p["proof"] = GoalProof{strconv.FormatInt(evidenceID, 10), reason, time.Now().UTC()}
	raw, e := json.Marshal(p)
	if e != nil {
		err = e
		return
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO exploration_edges(exploration_id,src_id,rel,dst_id) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, s.expID, evidenceID, RelProves, goalID); err != nil {
		return
	}
	if _, err = tx.ExecContext(ctx, `UPDATE exploration_nodes SET state='met', payload=$1,blocked_reason=NULL,content_version=content_version+1 WHERE id=$2 AND exploration_id=$3`, string(raw), goalID, s.expID); err != nil {
		return
	}
	var remaining int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*),COUNT(*) FILTER(WHERE state<>'met') FROM exploration_nodes WHERE exploration_id=$1 AND kind='goal'`, s.expID).Scan(&total, &remaining); err != nil {
		return
	}
	err = tx.Commit()
	allMet = err == nil && total > 0 && remaining == 0
	return
}
