package traffic

import (
	"context"
	"fmt"
	"io"
	"testing"
)

func TestVisitExportAllPagesAndBinary(t *testing.T) {
	tr, _ := openTraffic(t)
	bulkRecord(tr, "export.example.test", 1007, 5)
	seen := map[string]bool{}
	err := tr.VisitExport(context.Background(), PageQuery{RespMin: -1, RespMax: -1}, nil, func(m ExchangeMeta, e *EvidenceExchange) error {
		if seen[m.ID] {
			t.Fatal("duplicate capture")
		}
		seen[m.ID] = true
		b, err := io.ReadAll(e.Response)
		if err != nil {
			return err
		}
		if len(b) != 5 {
			return fmt.Errorf("truncated body")
		}
		return nil
	}, true)
	if err != nil || len(seen) != 1007 {
		t.Fatalf("export count=%d err=%v", len(seen), err)
	}
	if err = tr.VisitExport(context.Background(), PageQuery{RespMin: -1, RespMax: -1}, []string{"absent"}, func(ExchangeMeta, *EvidenceExchange) error { return nil }, true); err == nil {
		t.Fatal("missing capture exported successfully")
	}
}
