package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type CompanyReportTask struct {
	ID     int64
	Status string
}

type CompanyReportRetest struct {
	ID              int64
	FindingID       int64
	Status          string
	Verdict         string
	BaselineVersion *int64
	CreatedAt       time.Time
}

type CompanyReportSnapshot struct {
	CompanyID     int64
	CompanyName   string
	GeneratedAt   time.Time
	AssetCounts   map[string]int
	Tasks         []CompanyReportTask
	Findings      []*DBFinding
	Retests       []CompanyReportRetest
	MixedExcluded int
}

const companyOwnedAssets = `WITH owned AS (
 SELECT COALESCE(jsonb_agg(id),'[]'::jsonb) AS ids FROM assets WHERE company_id=$1
) `

// All relational sections share one read snapshot. Only findings whose entire
// nonempty asset anchor belongs to this company are included; unknown/mixed
// anchors cannot silently import another company's records.
func (d *DB) CompanyReport(ctx context.Context, id int64) (*CompanyReportSnapshot, error) {
	tx, err := d.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	out := &CompanyReportSnapshot{CompanyID: id, AssetCounts: map[string]int{}, Findings: []*DBFinding{}}
	if err = tx.QueryRowContext(ctx, `SELECT name,transaction_timestamp() FROM companies WHERE id=$1`, id).Scan(&out.CompanyName, &out.GeneratedAt); errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT type,count(*) FROM assets WHERE company_id=$1 GROUP BY type`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var typ string
		var count int
		if err = rows.Scan(&typ, &count); err != nil {
			rows.Close()
			return nil, err
		}
		out.AssetCounts[typ] = count
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	rows, err = tx.QueryContext(ctx, companyOwnedAssets+`SELECT `+findingSelectCols+`,COALESCE(f.report,'')
	 FROM findings f LEFT JOIN tasks t ON t.id=f.task_id CROSS JOIN owned
	 WHERE jsonb_typeof(f.asset_ids)='array' AND f.asset_ids<>'[]'::jsonb AND f.asset_ids<@owned.ids
	 ORDER BY f.id`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		f := &DBFinding{}
		var assets []byte
		if err = rows.Scan(&f.ID, &f.TaskID, &f.NodeID, &f.VulnClass, &f.Name, &f.Severity,
			&f.Summary, &f.Evidence, &f.Worker, &assets, &f.Status, &f.CreatedAt,
			&f.TaskDescription, &f.EvidenceVersion, &f.ReportEvidenceVersion, &f.TrafficCount,
			&f.DuplicateOf, &f.DuplicateReason, &f.Report); err != nil {
			rows.Close()
			return nil, err
		}
		if err = json.Unmarshal(assets, &f.AssetIDs); err != nil {
			rows.Close()
			return nil, err
		}
		// Shared task descriptions may describe other company scopes.
		// Keep the exact task ID for provenance, without importing that prose.
		f.TaskDescription = ""
		out.Findings = append(out.Findings, f)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	if err = tx.QueryRowContext(ctx, companyOwnedAssets+`SELECT count(*) FROM findings f CROSS JOIN owned
	 WHERE NOT (f.asset_ids<@owned.ids) AND EXISTS (
	 SELECT 1 FROM assets a WHERE a.company_id=$1 AND f.asset_ids @> jsonb_build_array(a.id))`, id).Scan(&out.MixedExcluded); err != nil {
		return nil, err
	}
	rows, err = tx.QueryContext(ctx, companyOwnedAssets+`SELECT t.id,t.status FROM tasks t CROSS JOIN owned WHERE t.deleted_at IS NULL AND (
	 EXISTS(SELECT 1 FROM task_scope s WHERE s.task_id=t.id AND s.company_id=$1)
	 OR EXISTS(SELECT 1 FROM assets a WHERE a.company_id=$1 AND t.id=ANY(a.task_ids))
	 OR EXISTS(SELECT 1 FROM findings f WHERE f.task_id=t.id AND f.asset_ids<>'[]'::jsonb AND f.asset_ids<@owned.ids)) ORDER BY t.id`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var task CompanyReportTask
		if err = rows.Scan(&task.ID, &task.Status); err != nil {
			rows.Close()
			return nil, err
		}
		out.Tasks = append(out.Tasks, task)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	visible := map[int64]bool{}
	for _, f := range out.Findings {
		visible[f.ID] = true
	}
	for _, f := range out.Findings {
		if f.DuplicateOf != nil && !visible[*f.DuplicateOf] {
			f.DuplicateOf, f.DuplicateReason = nil, ""
		}
		traffic, err := FindingTrafficTx(tx, f.ID)
		if err != nil {
			return nil, err
		}
		f.TrafficBindings = traffic.Bindings
		r := CompanyReportRetest{FindingID: f.ID}
		var raw []byte
		err = tx.QueryRowContext(ctx, `SELECT id,status,verdict,created_at,snapshot->'finding'->'evidence_version'
		 FROM finding_retests WHERE finding_id=$1 ORDER BY id DESC LIMIT 1`, f.ID).Scan(&r.ID, &r.Status, &r.Verdict, &r.CreatedAt, &raw)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &r.BaselineVersion)
		}
		out.Retests = append(out.Retests, r)
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}
