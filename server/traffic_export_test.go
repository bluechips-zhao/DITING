package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"github.com/Autumn-27/artex/traffic"
	"strings"
	"testing"
)

func TestHARPreservesBinaryAndUnknownTimings(t *testing.T) {
	data := []byte{0, 255, 13, 10, 42}
	var out bytes.Buffer
	err := writeHAREntry(&out, traffic.EvidenceExchange{ID: "exact-id", TS: 123, URL: "https://example.test/x?q=a", Method: "POST", Status: 200, ReqLen: 5, RespLen: 5, ReqHead: "POST /x HTTP/1.1\nX-Test: one\nX-Test: two", RespHead: "HTTP 200\nContent-Type: application/octet-stream", ContentType: "application/octet-stream", Request: bytes.NewReader(data), Response: bytes.NewReader(data)})
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err = json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err, out.String())
	}
	if decoded["time"] != float64(-1) {
		t.Fatal("invented timing")
	}
	res := decoded["response"].(map[string]any)
	content := res["content"].(map[string]any)
	body, err := base64.StdEncoding.DecodeString(content["text"].(string))
	if err != nil || !bytes.Equal(body, data) {
		t.Fatal("binary lost")
	}
	if !strings.Contains(out.String(), "exact-id") {
		t.Fatal("capture lineage lost")
	}
}

func TestHARRejectsTruncatedBody(t *testing.T) {
	var out bytes.Buffer
	if err := writeHAREntry(&out, traffic.EvidenceExchange{ReqLen: 2, RespLen: 0, Request: strings.NewReader("a"), Response: strings.NewReader("")}); err == nil {
		t.Fatal("truncated body accepted")
	}
}
