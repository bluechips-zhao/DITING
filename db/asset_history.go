package db

import (
	"context"
	"encoding/json"
	"time"
)

type AssetChange struct {
	ID         int64           `json:"id,string"`
	AssetID    int64           `json:"asset_id,string"`
	Kind       string          `json:"kind"`
	Previous   json.RawMessage `json:"previous"`
	Current    json.RawMessage `json:"current"`
	ObservedAt time.Time       `json:"observed_at"`
}

// History belongs to the stable asset ID, not to a guessed task execution.
// Task arrays describe links at observation time, not the actor of the change.
func (d *DB) AssetHistory(ctx context.Context, id, before int64) ([]AssetChange, error) {
	rows, err := d.QueryContext(ctx, `SELECT id,asset_id,kind,previous,current,observed_at FROM asset_change_history WHERE asset_id=$1 AND ($2::bigint=0 OR id<$2) ORDER BY id DESC LIMIT 50`, id, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AssetChange{}
	for rows.Next() {
		var v AssetChange
		var previous, current []byte
		if err := rows.Scan(&v.ID, &v.AssetID, &v.Kind, &previous, &current, &v.ObservedAt); err != nil {
			return nil, err
		}
		v.Previous, v.Current = previous, current
		out = append(out, v)
	}
	return out, rows.Err()
}
