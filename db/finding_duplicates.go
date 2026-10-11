package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

type FindingDuplicateLink struct {
	FindingID int64     `json:"finding_id"`
	PrimaryID int64     `json:"primary_id"`
	Reason    string    `json:"reason"`
	At        time.Time `json:"created_at"`
}
type DuplicateCandidate struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Summary string `json:"summary"`
	Reason  string `json:"reason"`
}

func (d *DB) FindingDuplicates(ctx context.Context, id int64) (*FindingDuplicateLink, []DuplicateCandidate, error) {
	var rel FindingDuplicateLink
	err := d.QueryRowContext(ctx, `SELECT finding_id,primary_id,reason,created_at FROM finding_duplicates WHERE finding_id=$1`, id).Scan(&rel.FindingID, &rel.PrimaryID, &rel.Reason, &rel.At)
	var ptr *FindingDuplicateLink
	if err == nil {
		ptr = &rel
	} else if err != sql.ErrNoRows {
		return nil, nil, err
	}
	rows, err := d.QueryContext(ctx, `SELECT b.id,COALESCE(b.name,''),b.summary,CASE WHEN a.evidence<>'' AND a.evidence=b.evidence THEN '相同类别与证据文本（仍需人工确认）' ELSE '相同类别且资产交集（仍需人工确认）' END FROM findings a JOIN findings b ON a.id<>b.id AND a.vulnclass<>'' AND a.vulnclass=b.vulnclass WHERE a.id=$1 AND (a.evidence<>'' AND a.evidence=b.evidence OR EXISTS(SELECT 1 FROM jsonb_array_elements(a.asset_ids) x WHERE b.asset_ids @> jsonb_build_array(x))) ORDER BY b.id DESC LIMIT 20`, id)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	out := []DuplicateCandidate{}
	for rows.Next() {
		var c DuplicateCandidate
		if err = rows.Scan(&c.ID, &c.Name, &c.Summary, &c.Reason); err != nil {
			return nil, nil, err
		}
		out = append(out, c)
	}
	return ptr, out, rows.Err()
}

// Relations are serialized to reject cycles/chains; neither record is deleted.
func (d *DB) LinkFindingDuplicate(ctx context.Context, id, primary int64, reason string) error {
	reason = strings.TrimSpace(reason)
	if id <= 0 || primary < 0 || id == primary || reason == "" {
		return errors.New("主发现编号和人工确认理由无效")
	}
	return d.WithEvidenceTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(7274021)`); err != nil {
			return err
		}
		var status string
		if err := tx.QueryRowContext(ctx, `SELECT status FROM findings WHERE id=$1 FOR UPDATE`, id).Scan(&status); err != nil {
			return err
		}
		previous := status
		var old string
		err := tx.QueryRowContext(ctx, `SELECT previous_status FROM finding_duplicates WHERE finding_id=$1`, id).Scan(&old)
		if err == nil {
			previous = old
		} else if err != sql.ErrNoRows {
			return err
		}
		action := "link"
		next := "duplicate"
		if primary == 0 {
			action = "unlink"
			next = previous
			if _, err = tx.Exec(`DELETE FROM finding_duplicates WHERE finding_id=$1`, id); err != nil {
				return err
			}
		} else {
			var found int64
			if err = tx.QueryRowContext(ctx, `SELECT id FROM findings WHERE id=$1 FOR UPDATE`, primary).Scan(&found); err != nil {
				return err
			}
			var conflict bool
			if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM finding_duplicates WHERE finding_id=$1 OR primary_id=$2)`, primary, id).Scan(&conflict); err != nil {
				return err
			}
			if conflict {
				return errors.New("不能关联形成循环或多层重复；请直接选择主记录")
			}
			if _, err = tx.Exec(`INSERT INTO finding_duplicates(finding_id,primary_id,reason,previous_status) VALUES($1,$2,$3,$4) ON CONFLICT(finding_id) DO UPDATE SET primary_id=$2,reason=$3,created_at=now()`, id, primary, reason, previous); err != nil {
				return err
			}
		}
		if _, _, _, _, err = SetFindingStatusTx(ctx, tx, id, next); err != nil {
			return err
		}
		_, err = tx.Exec(`INSERT INTO finding_duplicate_history(finding_id,primary_id,reason,action) VALUES($1,NULLIF($2,0),$3,$4)`, id, primary, reason, action)
		return err
	})
}
