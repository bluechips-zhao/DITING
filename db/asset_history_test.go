package db

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAssetHistoryMeaningfulChangesAndMigration(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	var id int64
	if err := d.QueryRow(`INSERT INTO assets(type,domain,task_ids,auth,extra) VALUES('subdomain','history.example.test',ARRAY[11]::bigint[],ARRAY['{"password":"never-snapshot-this"}'::jsonb],'{"secret":"never-snapshot-this"}') RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = d.Exec(`DELETE FROM assets WHERE id=$1`, id) })
	history := func() []AssetChange {
		rows, err := d.AssetHistory(t.Context(), id, 0)
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	first := history()
	if len(first) != 1 || first[0].Kind != "created" || len(first[0].Previous) > 0 {
		t.Fatalf("create: %+v", first)
	}
	if strings.Contains(string(first[0].Current), "never-snapshot-this") {
		t.Fatal("credential container copied")
	}
	if _, err := d.Exec(`UPDATE assets SET last_seen=now(),auth=ARRAY['{"password":"another-secret"}'::jsonb],extra='{"x":1}' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if len(history()) != 1 {
		t.Fatal("non-projected updates produced noise")
	}
	if _, err := d.Exec(`UPDATE assets SET record_value=ARRAY['127.0.0.2'],task_ids=ARRAY[11,22]::bigint[] WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	changed := history()
	if len(changed) != 2 || changed[0].Kind != "updated" {
		t.Fatalf("update: %+v", changed)
	}
	var old, cur map[string]any
	if err := json.Unmarshal(changed[0].Previous, &old); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(changed[0].Current, &cur); err != nil {
		t.Fatal(err)
	}
	if len(old["task_ids"].([]any)) != 1 || len(cur["task_ids"].([]any)) != 2 {
		t.Fatal("cross-task links lost")
	}
	if cur["task_ids"].([]any)[1] != "22" {
		t.Fatal("source IDs must be JSON strings to retain browser precision")
	}
	page, err := d.AssetHistory(t.Context(), id, changed[0].ID)
	if err != nil || len(page) != 1 || page[0].ID != first[0].ID {
		t.Fatal("cursor lost old record", err)
	}
	// Simulate an old asset without history and re-apply the idempotent schema.
	if _, err := d.Exec(`DELETE FROM asset_change_history WHERE asset_id=$1`, id); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err := d.Exec(schemaSQL); err != nil {
			t.Fatal(err)
		}
	}
	baseline := history()
	if len(baseline) != 1 || baseline[0].Kind != "baseline" || len(baseline[0].Previous) > 0 {
		t.Fatalf("false historical event: %+v", baseline)
	}
	if _, err := d.Exec(`DELETE FROM assets WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if len(history()) != 0 {
		t.Fatal("asset deletion retained history")
	}
}
