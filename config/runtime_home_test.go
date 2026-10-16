package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRuntimeHomeSeparatesConfigAndKeysFromExecutable(t *testing.T) {
	home := filepath.Join(t.TempDir(), "runtime")
	t.Setenv("ARTEX_HOME", home)
	t.Setenv("ARTEX_CONFIG", "")
	t.Setenv("ARTEX_SKILL_DIR", "")
	if err := ValidateHome(); err != nil {
		t.Fatal(err)
	}
	if BaseDir() != home || Path() != filepath.Join(home, "config.json") {
		t.Fatal("runtime home must anchor default config and keys")
	}
	if got := SkillDir(); got != filepath.Join(home, "skills") {
		t.Fatalf("skills: %s", got)
	}
	t.Setenv("ARTEX_CONFIG", filepath.Join(t.TempDir(), "external.json"))
	if BaseDir() != home {
		t.Fatal("config override must not relocate keys")
	}
	t.Setenv("ARTEX_HOME", "relative-home")
	if ValidateHome() == nil {
		t.Fatal("relative runtime directory must be rejected")
	}
	t.Setenv("ARTEX_HOME", "")
	if ValidateHome() != nil {
		t.Fatal("legacy empty runtime override must remain supported")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Dir(exe)
	if isGoRunDir(want) {
		want = "."
	}
	if BaseDir() != want {
		t.Fatal("default executable anchoring changed")
	}
}
