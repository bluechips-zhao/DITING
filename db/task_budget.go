package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var ErrTaskBudget = errors.New("任务累计预算不足，停止新增工作并收尾写回")

type TaskBudget struct {
	TaskID             int64  `json:"task_id"`
	TokenLimit         int64  `json:"token_limit"`
	ToolLimit          int64  `json:"tool_limit"`
	ReserveTokens      int64  `json:"reserve_tokens"`
	ReserveTools       int64  `json:"reserve_tools"`
	TokensUsed         int64  `json:"tokens_used"`
	TokensReserved     int64  `json:"tokens_reserved"`
	ToolsUsed          int64  `json:"tools_used"`
	UnreportedRequests int64  `json:"unreported_requests"`
	State              string `json:"state"`
}

const taskBudgetCols = `task_id,token_limit,tool_limit,reserve_tokens,reserve_tools,tokens_used,tokens_reserved,tools_used,unreported_requests,state`

func scanTaskBudget(row interface{ Scan(...any) error }) (b TaskBudget, err error) {
	err = row.Scan(&b.TaskID, &b.TokenLimit, &b.ToolLimit, &b.ReserveTokens, &b.ReserveTools, &b.TokensUsed, &b.TokensReserved, &b.ToolsUsed, &b.UnreportedRequests, &b.State)
	return
}
func (d *DB) TaskBudget(ctx context.Context, id int64) (TaskBudget, error) {
	b, err := scanTaskBudget(d.QueryRowContext(ctx, `SELECT `+taskBudgetCols+` FROM task_budgets WHERE task_id=$1`, id))
	if err == sql.ErrNoRows {
		return TaskBudget{TaskID: id, State: "active"}, nil
	}
	return b, err
}

// A interrupted request has unknown billing. Preserve the reservation as
// consumption, then release the in-flight marker so limits remain adjustable.
func (d *DB) RecoverTaskBudgetCalls(ctx context.Context) error {
	rows, err := d.QueryContext(ctx, `SELECT id FROM task_budget_calls WHERE state='reserved'`)
	if err != nil {
		return err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err = d.FinishTaskTokens(ctx, id, 0, false); err != nil {
			return err
		}
	}
	return nil
}
func (d *DB) SetTaskBudget(ctx context.Context, b TaskBudget) error {
	if b.TokenLimit < 0 || b.ToolLimit < 0 || b.ReserveTokens < 0 || b.ReserveTools < 0 || (b.TokenLimit > 0 && b.ReserveTokens >= b.TokenLimit) || (b.ToolLimit > 0 && b.ReserveTools >= b.ToolLimit) {
		return errors.New("预算和收尾预留无效（0 表示不限制）")
	}
	_, err := d.ExecContext(ctx, `INSERT INTO task_budgets(task_id,token_limit,tool_limit,reserve_tokens,reserve_tools,tokens_used,tools_used) SELECT $1,$2,$3,$4,$5,COALESCE((SELECT SUM(COALESCE(input_tokens,0)+COALESCE(output_tokens,0)) FROM activity WHERE exploration_id=(SELECT exploration_id FROM tasks WHERE id=$1) AND kind='result'),0)+COALESCE((SELECT SUM(COALESCE(a.input_tokens,0)+COALESCE(a.output_tokens,0)) FROM conversation_activities a JOIN conversations c ON c.id=a.conversation_id WHERE c.source_task_id=$1 AND a.kind='result'),0),(SELECT COUNT(*) FROM activity WHERE exploration_id=(SELECT exploration_id FROM tasks WHERE id=$1) AND kind='tool_use')+(SELECT COUNT(*) FROM conversation_activities a JOIN conversations c ON c.id=a.conversation_id WHERE c.source_task_id=$1 AND a.kind='tool_use') ON CONFLICT(task_id) DO UPDATE SET token_limit=$2,tool_limit=$3,reserve_tokens=$4,reserve_tools=$5,state=CASE WHEN ($2=0 OR task_budgets.tokens_used+task_budgets.tokens_reserved<$2-$4) AND ($3=0 OR task_budgets.tools_used<$3-$5) THEN 'active' ELSE 'settling' END,updated_at=now()`, b.TaskID, b.TokenLimit, b.ToolLimit, b.ReserveTokens, b.ReserveTools)
	return err
}

// Reservations serialize admission across workers and survive restart. Unknown
// provider usage is conservatively charged at the reservation, never refunded.
func (d *DB) ReserveTaskTokens(ctx context.Context, id, estimate int64) (callID int64, b TaskBudget, err error) {
	tx, e := d.BeginTx(ctx, nil)
	if e != nil {
		err = e
		return
	}
	defer tx.Rollback()
	b, e = scanTaskBudget(tx.QueryRowContext(ctx, `SELECT `+taskBudgetCols+` FROM task_budgets WHERE task_id=$1 FOR UPDATE`, id))
	if e == sql.ErrNoRows {
		return 0, TaskBudget{TaskID: id, State: "active"}, nil
	}
	if e != nil {
		err = e
		return
	}
	if b.TokenLimit == 0 {
		estimate = 0
	}
	if b.TokenLimit > 0 && b.TokensUsed+b.TokensReserved+estimate > b.TokenLimit {
		_, e = tx.Exec(`UPDATE task_budgets SET state='exhausted',updated_at=now() WHERE task_id=$1`, id)
		if e == nil {
			e = tx.Commit()
		}
		if e != nil {
			err = e
		} else {
			err = ErrTaskBudget
		}
		b.State = "exhausted"
		return
	}
	if b.State == "active" && b.TokenLimit > 0 && b.TokensUsed+b.TokensReserved+estimate > b.TokenLimit-b.ReserveTokens {
		b.State = "settling"
	}
	if e = tx.QueryRowContext(ctx, `INSERT INTO task_budget_calls(task_id,reserved) VALUES($1,$2) RETURNING id`, id, estimate).Scan(&callID); e != nil {
		err = e
		return
	}
	_, e = tx.Exec(`UPDATE task_budgets SET tokens_reserved=tokens_reserved+$2,state=$3,updated_at=now() WHERE task_id=$1`, id, estimate, b.State)
	if e == nil {
		e = tx.Commit()
	}
	err = e
	return
}
func (d *DB) FinishTaskTokens(ctx context.Context, callID, actual int64, reported bool) error {
	if callID == 0 {
		return nil
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var task, reserved int64
	var state string
	if err = tx.QueryRowContext(ctx, `SELECT task_id,reserved,state FROM task_budget_calls WHERE id=$1 FOR UPDATE`, callID).Scan(&task, &reserved, &state); err != nil {
		return err
	}
	if state != "reserved" {
		return tx.Commit()
	}
	state = "reconciled"
	unknown := 0
	if !reported {
		actual = reserved
		state = "unknown"
		unknown = 1
	}
	if actual < 0 {
		return fmt.Errorf("negative provider usage")
	}
	if _, err = tx.Exec(`UPDATE task_budget_calls SET charged=$2,state=$3 WHERE id=$1`, callID, actual, state); err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE task_budgets SET tokens_used=tokens_used+$2,tokens_reserved=GREATEST(0,tokens_reserved-$3),unreported_requests=unreported_requests+$4,state=CASE WHEN token_limit>0 AND tokens_used+$2+tokens_reserved-$3>=token_limit THEN 'exhausted' WHEN token_limit>0 AND tokens_used+$2+tokens_reserved-$3>=token_limit-reserve_tokens THEN 'settling' ELSE state END,updated_at=now() WHERE task_id=$1`, task, actual, reserved, unknown)
	if err != nil {
		return err
	}
	return tx.Commit()
}
func (d *DB) ConsumeTaskTool(ctx context.Context, id int64, settlement bool) (b TaskBudget, err error) {
	tx, e := d.BeginTx(ctx, nil)
	if e != nil {
		err = e
		return
	}
	defer tx.Rollback()
	b, e = scanTaskBudget(tx.QueryRowContext(ctx, `SELECT `+taskBudgetCols+` FROM task_budgets WHERE task_id=$1 FOR UPDATE`, id))
	if e == sql.ErrNoRows {
		return TaskBudget{TaskID: id, State: "active"}, nil
	}
	if e != nil {
		err = e
		return
	}
	limit := b.ToolLimit
	if !settlement {
		limit -= b.ReserveTools
	}
	blocked := b.State != "active" && !settlement || b.ToolLimit > 0 && b.ToolsUsed >= limit
	if blocked {
		b.State = "settling"
		if b.ToolLimit > 0 && b.ToolsUsed >= b.ToolLimit {
			b.State = "exhausted"
		}
		_, e = tx.Exec(`UPDATE task_budgets SET state=$2,updated_at=now() WHERE task_id=$1`, id, b.State)
	} else {
		b.ToolsUsed++
		_, e = tx.Exec(`UPDATE task_budgets SET tools_used=$2,updated_at=now() WHERE task_id=$1`, id, b.ToolsUsed)
	}
	if e == nil {
		e = tx.Commit()
	}
	err = e
	if err == nil && blocked {
		err = ErrTaskBudget
	}
	return
}
