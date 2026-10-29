package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

type RetestBaseline struct {
	Name                  string `json:"name"`
	Status                string `json:"status"`
	Summary               string `json:"summary"`
	Evidence              string `json:"evidence"`
	EvidenceVersion       *int64 `json:"evidence_version"`
	ReportEvidenceVersion *int64 `json:"report_evidence_version"`
}

type RetestComparison struct {
	FindingID              int64           `json:"finding_id,string"`
	RetestID               int64           `json:"retest_id,string"`
	Status                 string          `json:"status"`
	Verdict                string          `json:"verdict"`
	Summary                string          `json:"summary"`
	Evidence               string          `json:"evidence"`
	CreatedAt              time.Time       `json:"created_at"`
	FinishedAt             *time.Time      `json:"finished_at"`
	Baseline               *RetestBaseline `json:"baseline"`
	CurrentStatus          string          `json:"current_status"`
	CurrentEvidenceVersion int64           `json:"current_evidence_version"`
	SourceEvidenceChanged  *bool           `json:"source_evidence_changed"`
}

// The baseline is the immutable start snapshot, never today's finding prose.
// One statement reads the pair and current version; cross-finding IDs return nil.
func (d *DB) FindingRetestComparison(ctx context.Context, findingID, retestID int64) (*RetestComparison, error) {
	c := &RetestComparison{}
	var raw []byte
	err := d.QueryRowContext(ctx, `SELECT r.finding_id,r.id,r.status,r.verdict,r.summary,r.evidence,
	 r.created_at,r.finished_at,r.snapshot->'finding',f.status,f.evidence_version
	 FROM finding_retests r JOIN findings f ON f.id=r.finding_id
	 WHERE r.finding_id=$1 AND r.id=$2`, findingID, retestID).Scan(
		&c.FindingID, &c.RetestID, &c.Status, &c.Verdict, &c.Summary, &c.Evidence,
		&c.CreatedAt, &c.FinishedAt, &raw, &c.CurrentStatus, &c.CurrentEvidenceVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &c.Baseline); err != nil {
			return nil, err
		}
	}
	if c.Baseline != nil && c.Baseline.EvidenceVersion != nil {
		changed := *c.Baseline.EvidenceVersion != c.CurrentEvidenceVersion
		c.SourceEvidenceChanged = &changed
	}
	return c, nil
}
