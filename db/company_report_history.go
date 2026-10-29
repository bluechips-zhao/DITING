package db

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

type CompanyReportVersion struct {
	ID         int64     `json:"id,string"`
	CompanyID  int64     `json:"company_id,string"`
	Template   string    `json:"template"`
	SnapshotAt time.Time `json:"snapshot_at"`
	CreatedAt  time.Time `json:"created_at"`
	Content    string    `json:"content,omitempty"`
}

func (d *DB) SaveCompanyReport(ctx context.Context, companyID int64, template string, snapshotAt time.Time, redactedContent string) (*CompanyReportVersion, error) {
	v := &CompanyReportVersion{CompanyID: companyID, Template: template, SnapshotAt: snapshotAt}
	err := d.QueryRowContext(ctx, `INSERT INTO company_report_versions(company_id,template,snapshot_at,content) VALUES($1,$2,$3,$4) RETURNING id,created_at`, companyID, template, snapshotAt, redactedContent).Scan(&v.ID, &v.CreatedAt)
	return v, err
}

func (d *DB) CompanyReportHistory(ctx context.Context, companyID, before int64) ([]CompanyReportVersion, error) {
	rows, err := d.QueryContext(ctx, `SELECT id,company_id,template,snapshot_at,created_at FROM company_report_versions WHERE company_id=$1 AND ($2::bigint=0 OR id<$2) ORDER BY id DESC LIMIT 50`, companyID, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CompanyReportVersion{}
	for rows.Next() {
		var v CompanyReportVersion
		if err := rows.Scan(&v.ID, &v.CompanyID, &v.Template, &v.SnapshotAt, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (d *DB) CompanyReportVersion(ctx context.Context, companyID, versionID int64) (*CompanyReportVersion, error) {
	v := &CompanyReportVersion{}
	err := d.QueryRowContext(ctx, `SELECT id,company_id,template,snapshot_at,created_at,content FROM company_report_versions WHERE company_id=$1 AND id=$2`, companyID, versionID).Scan(&v.ID, &v.CompanyID, &v.Template, &v.SnapshotAt, &v.CreatedAt, &v.Content)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func (d *DB) DeleteCompanyReportVersion(ctx context.Context, companyID, versionID int64) (bool, error) {
	res, err := d.ExecContext(ctx, `DELETE FROM company_report_versions WHERE company_id=$1 AND id=$2`, companyID, versionID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}
