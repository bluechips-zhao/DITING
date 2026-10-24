package server

import "testing"

func TestRepetitionGuardKeepsLargeIntegerInputsDistinct(t *testing.T) {
	g := &repetitionGuard{}
	for range 3 {
		g.observe("Read", []byte(`{"asset_id":9007199254740992}`), []byte("same"), false)
	}
	if g.blocked("Read", []byte(`{"asset_id":9007199254740993}`)) {
		t.Fatal("different large integer identifiers must not share a repetition key")
	}
}

func TestRepetitionGuardDoesNotDiscardTrailingInput(t *testing.T) {
	g := &repetitionGuard{}
	for range 3 {
		g.observe("Read", []byte(`{"asset_id":1}`), []byte("same"), false)
	}
	if g.blocked("Read", []byte(`{"asset_id":1} {"asset_id":2}`)) {
		t.Fatal("malformed trailing data must not be normalized into a different valid request")
	}
}

func TestRepetitionGuardAllowsProgressAndStatefulWaiting(t *testing.T) {
	g := &repetitionGuard{}
	for range 3 {
		g.observe("Read", []byte(`{"b":2,"a":1}`), []byte("same"), false)
	}
	if !g.blocked("Read", []byte(`{"a":1,"b":2}`)) {
		t.Fatal("equivalent inputs not detected")
	}
	g.observe("Read", []byte(`{"a":1,"b":2}`), []byte("changed"), false)
	if g.blocked("Read", []byte(`{"a":1,"b":2}`)) {
		t.Fatal("changed output still blocked")
	}
	for range 5 {
		g.observe("TaskOutput", []byte(`{}`), []byte("pending"), false)
	}
	if g.blocked("TaskOutput", []byte(`{}`)) {
		t.Fatal("legitimate waiting blocked")
	}
	for range 3 {
		g.observe("Read", nil, []byte("same"), false)
	}
	g.observe("record_fact", nil, []byte("fact recorded"), false)
	if g.blocked("Read", nil) {
		t.Fatal("progress did not reset guard")
	}
}
