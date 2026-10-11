package db

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"
)

type ConversationCursor struct {
	Pin     *time.Time `json:"pin"`
	Updated time.Time  `json:"updated"`
	ID      int64      `json:"id"`
}

var ErrConversationCursor = errors.New("invalid cursor")

func (d *DB) SearchConversations(ctx context.Context, q, agent, cursor string, limit int) (items []*Conversation, next string, total int, err error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	var key ConversationCursor
	if cursor != "" {
		raw, e := base64.RawURLEncoding.DecodeString(cursor)
		if e != nil {
			err = ErrConversationCursor
			return
		}
		if e = json.Unmarshal(raw, &key); e != nil || key.ID <= 0 {
			err = ErrConversationCursor
			return
		}
	}
	where := ` WHERE ($1='' OR c.agent_key=$1) AND ($2='' OR strpos(lower(c.title),lower($2))>0 OR EXISTS(SELECT 1 FROM conversation_activities a WHERE a.conversation_id=c.id AND a.kind IN ('user','text','result','tool_result') AND strpos(lower(COALESCE(a.summary,'')||' '||COALESCE(a.detail,'')),lower($2))>0))`
	if err = d.QueryRowContext(ctx, `SELECT COUNT(*) FROM conversations c`+where, agent, q).Scan(&total); err != nil {
		return
	}
	rows, e := d.QueryContext(ctx, `SELECT `+convCols+` FROM conversations c`+where+` AND ($3=0 OR (COALESCE(pinned_at,'-infinity'::timestamptz),updated_at,id)<(COALESCE($4::timestamptz,'-infinity'::timestamptz),$5::timestamptz,$3)) ORDER BY COALESCE(pinned_at,'-infinity'::timestamptz) DESC,updated_at DESC,id DESC LIMIT $6`, agent, q, key.ID, key.Pin, key.Updated, limit+1)
	if e != nil {
		err = e
		return
	}
	defer rows.Close()
	items = []*Conversation{}
	for rows.Next() {
		c, e := scanConv(rows)
		if e != nil {
			err = e
			return
		}
		items = append(items, &c)
	}
	if err = rows.Err(); err != nil {
		return
	}
	if len(items) > limit {
		items = items[:limit]
		last := items[len(items)-1]
		raw, _ := json.Marshal(ConversationCursor{last.PinnedAt, last.UpdatedAt, last.ID})
		next = base64.RawURLEncoding.EncodeToString(raw)
	}
	return
}

// VisitConversationExport reads one repeatable snapshot including full bodies.
func (d *DB) VisitConversationExport(ctx context.Context, id int64, thinking, tools bool, visit func(Activity) error) error {
	tx, err := d.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, `SELECT id,COALESCE(worker,''),COALESCE(kind,''),COALESCE(tool,''),COALESCE(tool_use_id,''),is_error,COALESCE(summary,''),COALESCE(detail,''),created_at FROM conversation_activities WHERE conversation_id=$1 AND ($2 OR kind<>'thinking') AND ($3 OR kind NOT IN ('tool_use','tool_result')) ORDER BY id`, id, thinking, tools)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var a Activity
		if err = rows.Scan(&a.ID, &a.Worker, &a.Kind, &a.Tool, &a.ToolUseID, &a.IsError, &a.Summary, &a.Detail, &a.CreatedAt); err != nil {
			return err
		}
		if err = visit(a); err != nil {
			return err
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	rows.Close()
	return tx.Commit()
}
