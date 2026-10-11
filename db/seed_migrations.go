package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
)

func migrationTx(d *DB, flag string, apply func(*sql.Tx) error) (resultErr error) {
	defer func() {
		state, message := "ready", ""
		if resultErr != nil {
			state, message = "failed", resultErr.Error()
		}
		_, _ = d.Exec(`INSERT INTO migration_health(flag,state,error) VALUES($1,$2,$3) ON CONFLICT(flag) DO UPDATE SET state=$2,error=$3,updated_at=now()`, flag, state, message)
	}()
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`SELECT pg_advisory_xact_lock(hashtext($1))`, flag); err != nil {
		return err
	}
	var done bool
	if err = tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM settings WHERE key=$1 AND value='true')`, flag).Scan(&done); err != nil {
		return err
	}
	if done {
		return tx.Commit()
	}
	if err = apply(tx); err != nil {
		return err
	}
	if _, err = tx.Exec(`INSERT INTO settings(key,value) VALUES($1,'true') ON CONFLICT(key) DO UPDATE SET value='true'`, flag); err != nil {
		return err
	}
	return tx.Commit()
}

// MigrateDefaultPrompt preserves user-authored active prompts and all history.
// The new version and completion flag are committed in the same transaction.
func (d *DB) MigrateDefaultPrompt(flag, key, template string) error {
	if template == "" {
		return fmt.Errorf("empty default prompt for %s", key)
	}
	return migrationTx(d, flag, func(tx *sql.Tx) error {
		var id int64
		var current, by sql.NullString
		err := tx.QueryRow(`SELECT a.id,p.template_text,p.updated_by FROM agents a LEFT JOIN agent_prompts p ON p.id=a.current_prompt_id WHERE a.key=$1 FOR UPDATE OF a`, key).Scan(&id, &current, &by)
		if err == sql.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		if current.String == template || (current.Valid && by.String != "system") {
			return nil
		}
		var version int
		var promptID int64
		if err = tx.QueryRow(`SELECT COALESCE(MAX(version),0)+1 FROM agent_prompts WHERE agent_id=$1`, id).Scan(&version); err != nil {
			return err
		}
		if err = tx.QueryRow(`INSERT INTO agent_prompts(agent_id,version,template_text,note,updated_by) VALUES($1,$2,$3,'内置默认版本迁移','system') RETURNING id`, id, version, template).Scan(&promptID); err != nil {
			return err
		}
		_, err = tx.Exec(`UPDATE agents SET current_prompt_id=$1 WHERE id=$2`, promptID, id)
		return err
	})
}

// SeedReporter commits the agent, prompt, tool bindings, trigger and flag as one
// unit. A preexisting custom agent or intentional deletion remains untouched.
func (d *DB) SeedReporter(flag, prompt, message string, tools []string) error {
	return migrationTx(d, flag, func(tx *sql.Tx) error {
		var exists bool
		if err := tx.QueryRow(`SELECT EXISTS(SELECT 1 FROM agents WHERE key='reporter')`).Scan(&exists); err != nil {
			return err
		}
		if exists {
			return nil
		}
		var id, pid int64
		if err := tx.QueryRow(`INSERT INTO agents(key,name,description,role,builtin,enabled,trigger_run_mode,trigger_merge_mode,trigger_max_parallel) VALUES('reporter','报告撰写','漏洞详细报告撰写','assistant',false,true,'parallel','none',5) RETURNING id`).Scan(&id); err != nil {
			return err
		}
		if err := tx.QueryRow(`INSERT INTO agent_prompts(agent_id,version,template_text,note,updated_by) VALUES($1,1,$2,'内置默认','system') RETURNING id`, id, prompt).Scan(&pid); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE agents SET current_prompt_id=$1 WHERE id=$2`, pid, id); err != nil {
			return err
		}
		for _, key := range tools {
			result, err := tx.Exec(`UPDATE tools SET agents=CASE WHEN agents @> '["reporter"]'::jsonb THEN agents ELSE agents || '["reporter"]'::jsonb END WHERE key=$1`, key)
			if err != nil {
				return err
			}
			n, err := result.RowsAffected()
			if err != nil {
				return err
			}
			if n != 1 {
				return fmt.Errorf("reporter tool %s not seeded", key)
			}
		}
		names, _ := json.Marshal([]string{"report_finding"})
		_, err := tx.Exec(`INSERT INTO agent_triggers(agent_key,enabled,on_tool_call,tool_names,tool_call_message) VALUES('reporter',true,true,$1,$2)`, string(names), message)
		return err
	})
}

func (d *DB) MigrateReporterMessage(flag, old, new string) error {
	return migrationTx(d, flag, func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE agent_triggers SET tool_call_message=$1 WHERE agent_key='reporter' AND on_tool_call AND tool_call_message=$2`, new, old)
		return err
	})
}

func (d *DB) MigrateToolDefaults(flag string, tools []Tool) error {
	return migrationTx(d, flag, func(tx *sql.Tx) error {
		for _, t := range tools {
			if _, err := tx.Exec(`UPDATE tools SET description=$2,schema=$3,updated_at=now() WHERE key=$1 AND system`, t.Key, t.Description, string(t.Schema)); err != nil {
				return err
			}
		}
		return nil
	})
}

func (d *DB) MigrateToolBindings(flag, agent string, keys []string, remove bool) error {
	return migrationTx(d, flag, func(tx *sql.Tx) error {
		for _, key := range keys {
			var err error
			if remove {
				_, err = tx.Exec(`UPDATE tools SET agents=agents-$1 WHERE key=$2 AND agents ? $1`, agent, key)
			} else {
				_, err = tx.Exec(`UPDATE tools SET agents=agents || to_jsonb($1::text) WHERE key=$2 AND NOT(agents ? $1)`, agent, key)
			}
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// Extend optional evidence fields while preserving descriptions, bindings and
// any existing custom schema fields in installed tools.
func (d *DB) MigrateToolProperties(flag string, tools []Tool) error {
	return migrationTx(d, flag, func(tx *sql.Tx) error {
		for _, t := range tools {
			if _, err := tx.Exec(`UPDATE tools SET schema=jsonb_set(COALESCE(schema,'{}'::jsonb),'{properties}',COALESCE(schema->'properties','{}'::jsonb)||COALESCE($2::jsonb->'properties','{}'::jsonb)),updated_at=now() WHERE key=$1 AND system`, t.Key, string(t.Schema)); err != nil {
				return err
			}
		}
		return nil
	})
}
