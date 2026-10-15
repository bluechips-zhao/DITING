//go:build windows

package selfupdate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsSupervisorUpgradeAndRollback(t *testing.T) {
	p := testPaths(t)
	build := func(path, version string) {
		t.Helper()
		source := filepath.Join(t.TempDir(), "probe.go")
		body := fmt.Sprintf(`package main
import ("fmt"; "os"; "github.com/Autumn-27/artex/selfupdate")
func main() {
 if len(os.Args)>1 && os.Args[1]=="-h" { return }
 action, _ := selfupdate.Bootstrap()
 if action==selfupdate.Restart { os.Exit(selfupdate.ExitRestart) }
 selfupdate.Settle()
 fmt.Print(%q)
}
`, version)
		if err := os.WriteFile(source, []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		if output, err := exec.Command("go", "build", "-o", path, source).CombinedOutput(); err != nil {
			t.Fatalf("build: %v: %s", err, output)
		}
	}
	build(p.Current, "running-2.3.0")
	build(p.New, "running-2.3.1")
	sum, err := fileSHA256(p.New)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p.Sum, []byte(sum), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeMarker(p.Marker, marker{From: "2.3.0", To: "2.3.1"}); err != nil {
		t.Fatal(err)
	}
	launcher, err := os.ReadFile("../start.bat")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.Dir, "start.bat"), launcher, 0600); err != nil {
		t.Fatal(err)
	}
	run := func(want string) {
		t.Helper()
		command := exec.Command("cmd", "/c", "start.bat")
		command.Dir = p.Dir
		output, err := command.CombinedOutput()
		if err != nil || !strings.Contains(string(output), want) {
			t.Fatalf("supervisor restart: want %s, err=%v, output=%s", want, err, output)
		}
	}
	run("running-2.3.1")
	if _, ok := readMarker(p.Marker); ok {
		t.Fatal("stable startup did not settle upgrade")
	}
	if err := manualRollback(p); err != nil {
		t.Fatal(err)
	}
	run("running-2.3.0")
	if err := manualRollback(p); err != nil {
		t.Fatal(err)
	}
	run("running-2.3.1")
}
