package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
)

type repeatObservation struct {
	result [32]byte
	count  int
}
type repetitionGuard struct {
	mu   sync.Mutex
	seen map[string]repeatObservation
}

func repeatKey(name string, input []byte) string {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(input))
	decoder.UseNumber()
	if decoder.Decode(&value) == nil && decoder.Decode(new(any)) == io.EOF {
		if raw, err := json.Marshal(value); err == nil {
			input = raw
		}
	}
	return fmt.Sprintf("%s:%x", name, sha256.Sum256(input))
}
func repeatExempt(name string) bool {
	switch strings.ToLower(name) {
	case "sleep", "taskoutput", "task_output", "background_task_output":
		return true
	}
	return false
}
func (g *repetitionGuard) blocked(name string, input []byte) bool {
	if g == nil || repeatExempt(name) {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.seen[repeatKey(name, input)].count >= 3
}
func (g *repetitionGuard) observe(name string, input, result []byte, isErr bool) {
	if g == nil || repeatExempt(name) {
		return
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if !isErr && (name == "record_fact" || name == "report_finding" || name == "insert_assets") && !strings.Contains(string(result), `"reused":true`) {
		g.seen = nil
		return
	}
	if g.seen == nil {
		g.seen = map[string]repeatObservation{}
	}
	key := repeatKey(name, input)
	hash := sha256.Sum256(result)
	prev := g.seen[key]
	if prev.result != hash {
		prev = repeatObservation{result: hash}
	}
	prev.count++
	g.seen[key] = prev
}
