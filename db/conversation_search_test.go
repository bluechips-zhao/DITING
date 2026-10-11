package db

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestConversationSearchPaginationAndFullExport(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	key := fmt.Sprintf("search-%d", time.Now().UnixNano())
	var ids []int64
	defer func() { _, _ = d.DeleteConversations(ids) }()
	for i := 0; i < 105; i++ {
		c, e := d.CreateConversation(key, fmt.Sprintf("thread %d", i), nil)
		if e != nil {
			t.Fatal(e)
		}
		ids = append(ids, c.ID)
	}
	_, err = d.UpdateConversation(ids[0], ConversationPatch{Pinned: boolPtr(true)})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 205; i++ {
		_, err = d.AppendConvActivity(ids[1], Activity{Kind: "text", Summary: "short", Detail: fmt.Sprintf("完整历史 needle-%d", i)})
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = d.AppendConvActivity(ids[1], Activity{Kind: "thinking", Detail: "private reasoning"})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[int64]bool{}
	cursor := ""
	for {
		page, next, total, e := d.SearchConversations(context.Background(), "", key, cursor, 17)
		if e != nil {
			t.Fatal(e)
		}
		if total != 105 || len(page) > 17 {
			t.Fatalf("wrong bounded list %d/%d", len(page), total)
		}
		if cursor == "" && page[0].ID != ids[0] {
			t.Fatal("pin ordering lost")
		}
		for _, c := range page {
			if seen[c.ID] {
				t.Fatal("duplicate cursor row")
			}
			seen[c.ID] = true
		}
		if next == "" {
			break
		}
		cursor = next
	}
	if len(seen) != 105 {
		t.Fatal("pagination omitted conversations")
	}
	page, _, total, err := d.SearchConversations(context.Background(), "needle-204", key, "", 17)
	if err != nil || total != 1 || page[0].ID != ids[1] {
		t.Fatalf("full body search: %v total=%d", err, total)
	}
	count := 0
	last := ""
	err = d.VisitConversationExport(context.Background(), ids[1], false, true, func(a Activity) error { count++; last = a.Detail; return nil })
	if err != nil || count != 205 || !strings.Contains(last, "needle-204") {
		t.Fatalf("full export failed %d %s %v", count, last, err)
	}
	if _, _, _, err = d.SearchConversations(context.Background(), "", key, "invalid", 17); err != ErrConversationCursor {
		t.Fatal("bad cursor accepted")
	}
}

func boolPtr(v bool) *bool { return &v }
