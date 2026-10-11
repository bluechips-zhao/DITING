package traffic

import (
	"context"
	"errors"
)

// VisitExport shares the capture/delete lock for a stable index and body set.
// Output is staged by callers; a missing body cannot yield a successful package.
func (t *Traffic) VisitExport(ctx context.Context, f PageQuery, ids []string, visit func(ExchangeMeta, *EvidenceExchange) error, bodies bool) error {
	if t == nil {
		return errors.New("traffic unavailable")
	}
	if f.Body != "" {
		if _, _, ok := t.ftsFilter(f.Body); !ok {
			return errors.New("正文筛选需要可用的全文索引和至少三个字符")
		}
	}
	t.wmu.Lock()
	defer t.wmu.Unlock()
	one := func(m ExchangeMeta) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !bodies {
			return visit(m, nil)
		}
		return t.readEvidence(m.ID, func(e EvidenceExchange) error { return visit(m, &e) })
	}
	if len(ids) > 0 {
		seen := map[string]bool{}
		for _, id := range ids {
			if seen[id] {
				continue
			}
			seen[id] = true
			var m ExchangeMeta
			if err := t.db.QueryRowContext(ctx, `SELECT id,ts,host,method,url_template,url,status,content_type,resp_len,path FROM exchanges WHERE id=?`, id).Scan(&m.ID, &m.TS, &m.Host, &m.Method, &m.URLTemplate, &m.URL, &m.Status, &m.ContentType, &m.RespLen, &m.Path); err != nil {
				return err
			}
			if err := one(m); err != nil {
				return err
			}
		}
		return nil
	}
	for page := 0; ; page++ {
		items, _, err := t.Page(f, page, 500)
		if err != nil {
			return err
		}
		for _, m := range items {
			if err = one(m); err != nil {
				return err
			}
		}
		if len(items) < 500 {
			return nil
		}
	}
}
