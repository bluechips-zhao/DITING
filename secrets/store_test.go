package secrets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRestoreAndIsolation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.key")
	store, err := Open(path, false)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := store.Seal("llm", "test-api-key")
	if err != nil || sealed == "test-api-key" {
		t.Fatal("credential not encrypted")
	}
	restored, err := Open(path, true)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := restored.OpenValue("llm", sealed)
	if err != nil || plain != "test-api-key" {
		t.Fatal("restore cannot decrypt")
	}
	if _, err := restored.OpenValue("settings", sealed); err == nil {
		t.Fatal("scope substitution accepted")
	}
	wrong, _ := Open(filepath.Join(t.TempDir(), "other.key"), false)
	if _, err := wrong.OpenValue("llm", sealed); err == nil {
		t.Fatal("wrong key accepted")
	}
}

func TestDiskKeyHealthRejectsReplacementWithoutRewriting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.key")
	store, err := Open(path, false)
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.CheckKey(path); err != nil {
		t.Fatal(err)
	}
	wrong := []byte(strings.Repeat("x", 32))
	if err = os.WriteFile(path, wrong, 0600); err != nil {
		t.Fatal(err)
	}
	if err = store.CheckKey(path); err == nil {
		t.Fatal("different 32-byte key reported healthy")
	}
	actual, _ := os.ReadFile(path)
	if string(actual) != string(wrong) {
		t.Fatal("replacement key overwritten")
	}
	if err = os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	if err = store.CheckKey(path); err != nil {
		t.Fatal("restored original key rejected")
	}
	missing := filepath.Join(t.TempDir(), "secrets.key")
	if err = store.CheckKey(missing); err == nil {
		t.Fatal("missing key reported healthy")
	}
	if _, err = os.Stat(missing); !os.IsNotExist(err) {
		t.Fatal("health check generated replacement")
	}
}

func TestMissingAndDamagedKeysAreNeverReplaced(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.key")
	if _, err := Open(path, true); err == nil {
		t.Fatal("generated replacement for lost key")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("wrote replacement key")
	}
	if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path, false); err == nil {
		t.Fatal("accepted damaged key")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "broken" {
		t.Fatal("overwrote damaged key")
	}
}
