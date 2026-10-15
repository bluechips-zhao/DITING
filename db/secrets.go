package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Autumn-27/artex/secrets"
)

const secretEnvelope = "_diting_secret_v1"

var secretSettings = []string{"brave_search_api_key", "tavily_search_api_key", "global_proxy", "web_search_proxy"}

func secretSetting(key string) bool {
	for _, item := range secretSettings {
		if item == key {
			return true
		}
	}
	return false
}

type secretField struct {
	table, column string
	json          bool
}

var secretFields = []secretField{
	{"llm_profiles", "api_key", false}, {"llm_profiles", "proxy", false}, {"llm_profiles", "base_url", false},
	{"notification_channels", "config", true},
	{"mcp_servers", "args", true}, {"mcp_servers", "env", true}, {"mcp_servers", "url", false},
}

func encodedSecret(value string, isJSON bool) (string, bool) {
	if !isJSON {
		return value, strings.HasPrefix(value, secrets.Prefix)
	}
	var envelope map[string]json.RawMessage
	if json.Unmarshal([]byte(value), &envelope) != nil || len(envelope) != 1 {
		return value, false
	}
	var sealed string
	if json.Unmarshal(envelope[secretEnvelope], &sealed) != nil {
		return value, false
	}
	return sealed, strings.HasPrefix(sealed, secrets.Prefix)
}

func protectValue(store *secrets.Store, scope, value string, isJSON bool) (string, error) {
	if store == nil || value == "" {
		return value, nil
	}
	sealed, err := store.Seal(scope, value)
	if err != nil {
		return "", err
	}
	if !isJSON {
		return sealed, nil
	}
	data, err := json.Marshal(map[string]string{secretEnvelope: sealed})
	return string(data), err
}

func revealValue(store *secrets.Store, scope, value string, isJSON bool) (string, error) {
	sealed, encrypted := encodedSecret(value, isJSON)
	if !encrypted {
		return value, nil
	}
	if store == nil {
		return "", errors.New("凭据已加密；必须配置原 secrets.key 才能读取")
	}
	return store.OpenValue(scope, sealed)
}

func (d *DB) protect(scope, value string, isJSON bool) (string, error) {
	d.secretMu.RLock()
	defer d.secretMu.RUnlock()
	return protectValue(d.secretStore, scope, value, isJSON)
}

func (d *DB) reveal(scope, value string, isJSON bool) (string, error) {
	d.secretMu.RLock()
	defer d.secretMu.RUnlock()
	return revealValue(d.secretStore, scope, value, isJSON)
}

// CredentialKeyReady verifies that disk and in-memory keys still match.
func (d *DB) CredentialKeyReady(path string) bool {
	d.secretMu.RLock()
	defer d.secretMu.RUnlock()
	return d.secretStore != nil && d.secretStore.CheckKey(path) == nil
}

// ConfigureSecrets validates the key and migrates all credential fields atomically.
// Called before starting the server, with the key outside the browsable workspace.
func (d *DB) ConfigureSecrets(keyFile string) error {
	d.secretMu.Lock()
	defer d.secretMu.Unlock()
	tx, err := d.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock($1)`, schemaMigrationLockKey); err != nil {
		return err
	}
	if _, err := tx.Exec(`LOCK TABLE llm_profiles, settings, notification_channels, mcp_servers IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return err
	}
	type change struct {
		table, column, scope, value string
		id                          any
		isJSON                      bool
	}
	var values []change
	encryptedExists := false
	for _, field := range secretFields {
		rows, err := tx.Query(`SELECT id, COALESCE(` + field.column + `::text,'') FROM ` + field.table + ` ORDER BY id`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id int64
			var value string
			if err := rows.Scan(&id, &value); err != nil {
				rows.Close()
				return err
			}
			_, encrypted := encodedSecret(value, field.json)
			encryptedExists = encryptedExists || encrypted
			values = append(values, change{field.table, field.column, field.table + "." + field.column, value, id, field.json})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	for _, key := range secretSettings {
		var value string
		err := tx.QueryRow(`SELECT COALESCE(value,'') FROM settings WHERE key=$1`, key).Scan(&value)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return err
		}
		_, encrypted := encodedSecret(value, false)
		encryptedExists = encryptedExists || encrypted
		values = append(values, change{"settings", "value", "settings." + key, value, key, false})
	}
	store, err := secrets.Open(keyFile, encryptedExists)
	if err != nil {
		return err
	}
	for _, item := range values {
		plain, err := revealValue(store, item.scope, item.value, item.isJSON)
		if err != nil {
			return fmt.Errorf("%s: %w", item.scope, err)
		}
		_, encrypted := encodedSecret(item.value, item.isJSON)
		if encrypted || plain == "" {
			continue
		}
		protected, err := protectValue(store, item.scope, plain, item.isJSON)
		if err != nil {
			return err
		}
		idColumn := "id"
		if item.table == "settings" {
			idColumn = "key"
		}
		if _, err := tx.Exec(`UPDATE `+item.table+` SET `+item.column+`=$1 WHERE `+idColumn+`=$2`, protected, item.id); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	d.secretStore = store
	return nil
}
