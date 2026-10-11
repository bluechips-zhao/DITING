package db

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestAssetExportBeyondPagePreservesSnapshot(t *testing.T) {
	d, e := Open(testDSN(t))
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	key := fmt.Sprintf("export-%d.test", time.Now().UnixNano())
	defer d.Exec(`DELETE FROM assets WHERE root_domain=$1`, key)
	for i := 0; i < 513; i++ {
		_, e = d.Assets().UpsertSubdomain(UpsertSubdomainReq{Domain: fmt.Sprintf("n%d.%s", i, key)})
		if e != nil {
			t.Fatal(e)
		}
	}
	count := 0
	seen := map[int64]bool{}
	e = d.Assets().VisitAssetsExport(context.Background(), `root_domain="`+key+`"`, "subdomain", 0, 0, func(a *Asset) error {
		count++
		if seen[a.ID] {
			t.Fatal("duplicate export")
		}
		seen[a.ID] = true
		return nil
	})
	if e != nil || count != 513 {
		t.Fatalf("full export=%d %v", count, e)
	}
}
