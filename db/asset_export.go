package db

import (
	"context"
	"database/sql"
	"fmt"
)

// VisitAssetsExport uses a single snapshot with bounded batches; edits during
// export cannot move rows across pages or omit them.
func (s *AssetStore) VisitAssetsExport(ctx context.Context, dsl, typ string, taskID, companyID int64, visit func(*Asset) error) error {
	n, err := ParseDSL(dsl)
	if err != nil {
		return err
	}
	where, args, err := buildDSLWhere(n)
	if err != nil {
		return err
	}
	add := func(clause string, v any) { args = append(args, v); where += fmt.Sprintf(clause, len(args)) }
	if typ != "" {
		add(" AND type=$%d", typ)
	}
	if taskID > 0 {
		add(" AND $%d=ANY(task_ids)", taskID)
	}
	if companyID > 0 {
		add(" AND company_id=$%d", companyID)
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var last int64
	for {
		a := append(append([]any{}, args...), last)
		rows, err := tx.QueryContext(ctx, assetSelectCols+" WHERE "+where+fmt.Sprintf(" AND id>$%d ORDER BY id LIMIT 512", len(a)), a...)
		if err != nil {
			return err
		}
		items, err := scanAssets(rows)
		rows.Close()
		if err != nil {
			return err
		}
		if len(items) == 0 {
			break
		}
		for _, item := range items {
			if err = visit(item); err != nil {
				return err
			}
			last = item.ID
		}
	}
	return tx.Commit()
}
