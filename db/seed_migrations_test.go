package db

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestMigrationRollbackRetryAndCustomPrompt(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	flag := fmt.Sprintf("test_migration_%d", time.Now().UnixNano())
	key := fmt.Sprintf("migration_%d", time.Now().UnixNano())
	t.Cleanup(func() { _, _ = d.Exec(`DELETE FROM settings WHERE key=$1`, flag); _ = d.DeleteAgent(key) })
	err = migrationTx(d, flag, func(tx *sql.Tx) error {
		_, e := tx.Exec(`INSERT INTO settings(key,value) VALUES($1,'partial')`, flag)
		if e != nil {
			return e
		}
		return errors.New("injected failure")
	})
	if err == nil {
		t.Fatal("failure not propagated")
	}
	if _, ok, _ := d.GetSetting(flag); ok {
		t.Fatal("partial migration committed")
	}
	a, err := d.CreateAgent(key, "test", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.SavePrompt(a.ID, "custom prompt", "user edit", "user"); err != nil {
		t.Fatal(err)
	}
	if err = d.MigrateDefaultPrompt(flag, key, "new default"); err != nil {
		t.Fatal(err)
	}
	if cur, _ := d.CurrentPrompt(a.ID); cur != "custom prompt" {
		t.Fatal("custom prompt overwritten")
	}
	if v, _, _ := d.GetSetting(flag); v != "true" {
		t.Fatal("successful retry not marked")
	}
}
