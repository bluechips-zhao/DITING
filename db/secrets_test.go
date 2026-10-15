package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Credential migration must never alter the shared regression database.
func isolatedSecretsDB(t *testing.T) (*DB, string) {
	t.Helper()
	u, err := url.Parse(os.Getenv("ARTEX_PG_DSN"))
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost") {
		t.Skip("requires explicit local ARTEX_PG_DSN with database creation permission")
	}
	u.Path = "/postgres"
	admin, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("diting_secret_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec(`CREATE DATABASE "` + name + `"`); err != nil {
		admin.Close()
		t.Fatal(err)
	}
	u.Path = "/" + name
	d, err := Open(u.String())
	t.Cleanup(func() {
		if d != nil {
			d.Close()
		}
		if _, err := admin.Exec(`DROP DATABASE "` + name + `"`); err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	if err != nil {
		t.Fatal(err)
	}
	return d, u.String()
}

func TestCredentialMigrationRoundTrip(t *testing.T) {
	d, dsn := isolatedSecretsDB(t)
	ctx := context.Background()
	p := &LLMProfile{Name: "migration", Format: "openai", Model: "fixture", APIKey: "fixture-model-secret", BaseURL: "https://fixture.invalid/api?key=fixture-url-secret", Proxy: "http://user:fixture-proxy-secret@localhost:8888", Streaming: true}
	id, err := d.SaveProfile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err = d.SetSetting("brave_search_api_key", "fixture-search-secret"); err != nil {
		t.Fatal(err)
	}
	channel := &NotificationChannel{Name: "migration", Kind: "webhook", Mode: NotifyModeRealtime, Config: json.RawMessage(`{"url":"https://fixture.invalid/fixture-notify-secret"}`), Filter: json.RawMessage(`{}`)}
	cid, err := d.SaveNotificationChannel(ctx, channel)
	if err != nil {
		t.Fatal(err)
	}
	mcp := &MCPServer{Name: "migration", Transport: "stdio", Command: "fixture", Args: json.RawMessage(`["fixture-arg-secret"]`), Env: json.RawMessage(`{"KEY":"fixture-env-secret"}`), URL: "https://fixture.invalid/fixture-mcp-secret"}
	mid, err := d.SaveMCP(mcp)
	if err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(t.TempDir(), "secrets.key")
	if err = d.ConfigureSecrets(key); err != nil {
		t.Fatal(err)
	}
	if !d.CredentialKeyReady(key) {
		t.Fatal("configured key not ready")
	}
	if d.CredentialKeyReady(filepath.Join(t.TempDir(), "missing.key")) {
		t.Fatal("missing key ready")
	}
	var before string
	if err = d.QueryRow(`SELECT api_key FROM llm_profiles WHERE id=$1`, id).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(before, "diting:v1:") {
		t.Fatal("model key remains plaintext")
	}
	for _, field := range secretFields {
		rows, err := d.Query(`SELECT COALESCE(` + field.column + `::text,'') FROM ` + field.table)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var value string
			if err = rows.Scan(&value); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(value, "fixture-") {
				t.Errorf("plaintext in %s.%s", field.table, field.column)
			}
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		rows.Close()
	}
	if err = d.ConfigureSecrets(key); err != nil {
		t.Fatal(err)
	}
	var after string
	d.QueryRow(`SELECT api_key FROM llm_profiles WHERE id=$1`, id).Scan(&after)
	if before != after {
		t.Fatal("idempotent migration rewrote ciphertext")
	}
	got, err := d.ProfileByID(id)
	if err != nil || got.APIKey != p.APIKey || got.Proxy != p.Proxy || got.BaseURL != p.BaseURL {
		t.Fatalf("profile round trip: %v", err)
	}
	got.APIKey = ""
	got.Name = "updated"
	if _, err = d.SaveProfile(got); err != nil {
		t.Fatal(err)
	}
	got, err = d.ProfileByID(id)
	if err != nil || got.APIKey != p.APIKey {
		t.Fatal("blank update lost key")
	}
	if err = d.SetSetting("brave_search_api_key", "fixture-new-search-secret"); err != nil {
		t.Fatal(err)
	}
	value, ok, err := d.GetSetting("brave_search_api_key")
	if err != nil || !ok || value != "fixture-new-search-secret" {
		t.Fatalf("setting round trip: %v", err)
	}
	d.QueryRow(`SELECT value FROM settings WHERE key='brave_search_api_key'`).Scan(&value)
	if !strings.HasPrefix(value, "diting:v1:") {
		t.Fatal("new search write remains plaintext")
	}
	ch, err := d.NotificationChannelByID(ctx, cid)
	if err != nil || !strings.Contains(string(ch.Config), "fixture-notify-secret") {
		t.Fatalf("notification round trip: %v", err)
	}
	ch.Config = json.RawMessage(`{"url":"https://fixture.invalid/fixture-notify-new"}`)
	if _, err = d.SaveNotificationChannel(ctx, ch); err != nil {
		t.Fatal(err)
	}
	var rawConfig string
	if err = d.QueryRow(`SELECT config::text FROM notification_channels WHERE id=$1`, cid).Scan(&rawConfig); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rawConfig, "fixture-notify-new") || !strings.Contains(rawConfig, secretEnvelope) {
		t.Fatal("notification update remains plaintext")
	}
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	channels, err := d.listEnabledNotificationChannelsTx(ctx, tx)
	tx.Rollback()
	if err != nil {
		t.Fatal(err)
	}
	if len(channels) != 1 || !strings.Contains(string(channels[0].Config), "fixture-notify-new") {
		t.Fatal("notification fanout cannot decrypt config")
	}
	mcp.ID = mid
	mcp.Env = json.RawMessage(`{"KEY":"fixture-env-updated"}`)
	if _, err = d.SaveMCP(mcp); err != nil {
		t.Fatal(err)
	}
	if err = d.QueryRow(`SELECT env::text FROM mcp_servers WHERE id=$1`, mid).Scan(&rawConfig); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(rawConfig, "fixture-env-updated") || !strings.Contains(rawConfig, secretEnvelope) {
		t.Fatal("MCP update remains plaintext")
	}
	servers, err := d.ListMCP()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range servers {
		if s.ID == mid {
			found = strings.Contains(string(s.Env), "fixture-env-updated") && strings.Contains(string(s.Args), "fixture-arg-secret") && s.URL == mcp.URL
		}
	}
	if !found {
		t.Fatal("MCP round trip failed")
	}
	other, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	if _, err = other.ProfileByID(id); err == nil {
		t.Fatal("encrypted key read without key succeeded")
	}
	if err = other.ConfigureSecrets(key); err != nil {
		t.Fatal(err)
	}
	got, err = other.ProfileByID(id)
	if err != nil || got.APIKey != p.APIKey {
		t.Fatal("restart lost key")
	}
	// A late invalid ciphertext must roll back earlier plaintext migrations.
	if _, err = d.Exec(`UPDATE llm_profiles SET proxy='fixture-late-plaintext' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if _, err = d.Exec(`UPDATE mcp_servers SET url='diting:v1:corrupt' WHERE id=$1`, mid); err != nil {
		t.Fatal(err)
	}
	if err = other.ConfigureSecrets(key); err == nil {
		t.Fatal("corrupt ciphertext accepted")
	}
	d.QueryRow(`SELECT proxy FROM llm_profiles WHERE id=$1`, id).Scan(&value)
	if value != "fixture-late-plaintext" {
		t.Fatal("failed migration was partially committed")
	}
	missing := filepath.Join(t.TempDir(), "missing.key")
	if err = other.ConfigureSecrets(missing); err == nil {
		t.Fatal("missing key was regenerated")
	}
	if _, err = os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("missing key file created")
	}
	wrong := filepath.Join(t.TempDir(), "wrong.key")
	wrongBytes := []byte(strings.Repeat("x", 32))
	if err = os.WriteFile(wrong, wrongBytes, 0600); err != nil {
		t.Fatal(err)
	}
	if err = other.ConfigureSecrets(wrong); err == nil {
		t.Fatal("wrong key accepted")
	}
	actual, _ := os.ReadFile(wrong)
	if string(actual) != string(wrongBytes) {
		t.Fatal("wrong key overwritten")
	}
}

func TestEncryptedBackupRestore(t *testing.T) {
	pgBin := os.Getenv("DITING_TEST_PG_BIN")
	if pgBin == "" {
		t.Skip("requires PostgreSQL CLI directory in DITING_TEST_PG_BIN")
	}
	pwsh, err := exec.LookPath("pwsh")
	if err != nil {
		t.Skip("requires PowerShell 7")
	}
	d, dsn := isolatedSecretsDB(t)
	root := t.TempDir()
	key := filepath.Join(root, "secrets.key")
	id, err := d.SaveProfile(&LLMProfile{Name: "backup", Format: "openai", Model: "fixture", APIKey: "fixture-backup-secret"})
	if err != nil {
		t.Fatal(err)
	}
	if err = d.ConfigureSecrets(key); err != nil {
		t.Fatal(err)
	}
	if err = d.Close(); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(root, "data")
	if err = os.Mkdir(data, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(data, "fixture.txt"), []byte("fixture workspace"), 0600); err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(dsn)
	u.Path = "/postgres"
	admin, err := sql.Open("pgx", u.String())
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	name := fmt.Sprintf("diting_restore_test_%d", time.Now().UnixNano())
	if _, err = admin.Exec(`CREATE DATABASE "` + name + `"`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.Exec(`DROP DATABASE "` + name + `"`); err != nil {
			t.Error(err)
		}
	}()
	u.Path = "/" + name
	restoredDSN := u.String()
	script, _ := filepath.Abs(filepath.Join("..", "scripts", "backup.ps1"))
	pkg := filepath.Join(root, "backup.dbak")
	run := func(mode string, extra ...string) error {
		args := []string{"-NoProfile", "-File", script, "-Mode", mode, "-Package", pkg, "-PgBin", pgBin}
		args = append(args, extra...)
		cmd := exec.Command(pwsh, args...)
		cmd.Env = append(os.Environ(), "ARTEX_PG_DSN="+dsn, "DITING_RESTORE_DSN="+restoredDSN, "DITING_BACKUP_PASSPHRASE=fixture-backup-passphrase-32chars")
		output, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s: %w: %s", mode, err, output)
		}
		return nil
	}
	if err = run("Backup", "-DataDir", data, "-OfflineAcknowledged"); err == nil {
		t.Fatal("backup accepted encrypted DB without key plan")
	}
	wrong := filepath.Join(root, "wrong.key")
	if err = os.WriteFile(wrong, []byte(strings.Repeat("x", 32)), 0600); err != nil {
		t.Fatal(err)
	}
	if err = run("Backup", "-DataDir", data, "-OfflineAcknowledged", "-IncludeLocalCredentials", "-EncryptionKey", wrong); err == nil {
		t.Fatal("backup accepted wrong key")
	}
	if _, err = os.Stat(pkg); !os.IsNotExist(err) {
		t.Fatal("failed backup left output")
	}
	if err = run("Backup", "-DataDir", data, "-OfflineAcknowledged", "-IncludeLocalCredentials", "-EncryptionKey", key); err != nil {
		t.Fatal(err)
	}
	if err = run("Preview"); err != nil {
		t.Fatal(err)
	}
	restoreDir := filepath.Join(root, "restored")
	if err = run("Restore", "-DestinationDir", restoreDir, "-OfflineAcknowledged"); err != nil {
		t.Fatal(err)
	}
	restored, err := Open(restoredDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	if err = restored.ConfigureSecrets(filepath.Join(restoreDir, "local", "secrets.key")); err != nil {
		t.Fatal(err)
	}
	profile, err := restored.ProfileByID(id)
	if err != nil || profile.APIKey != "fixture-backup-secret" {
		t.Fatal("restored key cannot decrypt credentials")
	}
	actual, err := os.ReadFile(filepath.Join(restoreDir, "data", "fixture.txt"))
	if err != nil || string(actual) != "fixture workspace" {
		t.Fatal("workspace restore failed")
	}
}
