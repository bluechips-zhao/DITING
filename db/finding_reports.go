package db

import (
	"context"
	"database/sql"
	"time"
)

type FindingReportJob struct {
	ID              int64     `json:"id"`
	FindingID       int64     `json:"finding_id"`
	EvidenceVersion int64     `json:"evidence_version"`
	ConversationID  *int64    `json:"conversation_id"`
	State           string    `json:"state"`
	Error           string    `json:"error"`
	At              time.Time `json:"created_at"`
	Updated         time.Time `json:"updated_at"`
}
type FindingReportVersion struct {
	ID              int64     `json:"id"`
	EvidenceVersion int64     `json:"evidence_version"`
	Report          string    `json:"report"`
	At              time.Time `json:"created_at"`
}

const reportJobCols = `id,finding_id,evidence_version,conversation_id,state,error,created_at,updated_at`

func scanReportJob(row interface{ Scan(...any) error }) (j FindingReportJob, err error) {
	err = row.Scan(&j.ID, &j.FindingID, &j.EvidenceVersion, &j.ConversationID, &j.State, &j.Error, &j.At, &j.Updated)
	return
}
func (d *DB) QueueFindingReport(ctx context.Context, id int64) (j FindingReportJob, created bool, err error) {
	err = d.WithEvidenceTx(ctx, func(tx *sql.Tx) error {
		var version int64
		if e := tx.QueryRowContext(ctx, `SELECT evidence_version FROM findings WHERE id=$1 FOR UPDATE`, id).Scan(&version); e != nil {
			return e
		}
		var e error
		j, e = scanReportJob(tx.QueryRowContext(ctx, `SELECT `+reportJobCols+` FROM finding_report_jobs WHERE finding_id=$1 AND state IN ('queued','running')`, id))
		if e == nil {
			return nil
		}
		if e != sql.ErrNoRows {
			return e
		}
		j, e = scanReportJob(tx.QueryRowContext(ctx, `INSERT INTO finding_report_jobs(finding_id,evidence_version) VALUES($1,$2) RETURNING `+reportJobCols, id, version))
		created = e == nil
		return e
	})
	return
}
func (d *DB) StartFindingReport(ctx context.Context, id, conv int64) error {
	_, err := d.ExecContext(ctx, `UPDATE finding_report_jobs SET state='running',conversation_id=$2,updated_at=now() WHERE id=$1 AND state='queued'`, id, conv)
	return err
}
func (d *DB) FinishFindingReport(ctx context.Context, id int64, reason string) error {
	_, err := d.ExecContext(ctx, `UPDATE finding_report_jobs j SET state=CASE WHEN $2='' AND EXISTS(SELECT 1 FROM finding_report_versions v WHERE v.finding_id=j.finding_id AND v.evidence_version=j.evidence_version AND v.created_at>=j.created_at) THEN 'ready' ELSE 'failed' END,error=CASE WHEN $2<>'' THEN $2 WHEN EXISTS(SELECT 1 FROM finding_report_versions v WHERE v.finding_id=j.finding_id AND v.evidence_version=j.evidence_version AND v.created_at>=j.created_at) THEN '' ELSE '生成过程结束但未保存匹配证据版本的报告，请查看过程并重试' END,updated_at=now() WHERE j.id=$1 AND j.state IN ('queued','running')`, id, reason)
	return err
}
func (d *DB) FindingReportStatus(ctx context.Context, id int64, before ...int64) (jobs []FindingReportJob, versions []FindingReportVersion, err error) {
	cursor := int64(0)
	if len(before) > 0 {
		cursor = before[0]
	}
	jobs = []FindingReportJob{}
	versions = []FindingReportVersion{}
	rows, err := d.QueryContext(ctx, `SELECT id,finding_id,evidence_version,conversation_id,CASE WHEN state='ready' AND evidence_version<>(SELECT evidence_version FROM findings WHERE id=$1) THEN 'stale' ELSE state END,error,created_at,updated_at FROM finding_report_jobs WHERE finding_id=$1 AND ($2=0 OR id<$2) ORDER BY id DESC LIMIT 20`, id, cursor)
	if err != nil {
		return
	}
	for rows.Next() {
		j, e := scanReportJob(rows)
		if e != nil {
			rows.Close()
			err = e
			return
		}
		jobs = append(jobs, j)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return
	}
	rows, err = d.QueryContext(ctx, `SELECT id,evidence_version,report,created_at FROM finding_report_versions WHERE finding_id=$1 ORDER BY id DESC LIMIT 20`, id)
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var v FindingReportVersion
		if err = rows.Scan(&v.ID, &v.EvidenceVersion, &v.Report, &v.At); err != nil {
			return
		}
		versions = append(versions, v)
	}
	err = rows.Err()
	return
}

func (d *DB) RecoverInterruptedReportJobs() error {
	_, err := d.Exec(`UPDATE finding_report_jobs SET state='failed',error='服务重启中断了生成；可重新生成，原报告保留',updated_at=now() WHERE state IN ('queued','running')`)
	return err
}

func (d *DB) ReportJobForConversation(ctx context.Context, id int64) (*FindingReportJob, error) {
	j, err := scanReportJob(d.QueryRowContext(ctx, `SELECT `+reportJobCols+` FROM finding_report_jobs WHERE conversation_id=$1 ORDER BY id DESC LIMIT 1`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &j, nil
}
