package intercept

import (
	"context"
	"github.com/Autumn-27/artex/db"
	"testing"
)

func TestAskCannotAllowCanceledWorkEvenWithReadyDecision(t *testing.T) {
	dsn, _, err := db.DSN()
	if err != nil {
		t.Skip("explicit test postgres required")
	}
	d, err := db.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	defer d.Exec(`DELETE FROM intercept_pending WHERE task_id='cancel-test'`)
	i := New(d)
	for _, timed := range []bool{false, true} {
		for range 20 {
			ctx, cancel := context.WithCancel(t.Context())
			ctx = WithTaskContext(ctx, "cancel-test", "test", func(db.Activity) {
				rows, err := d.ListPendingIntercepts()
				if err != nil {
					t.Fatal(err)
				}
				for _, row := range rows {
					if row.TaskID == nil || *row.TaskID != "cancel-test" {
						continue
					}
					// Make both branches ready before HandleAsk enters select.
					cancel()
					if err := i.Decide(row.ID, true); err != nil {
						t.Fatal(err)
					}
				}
			})
			if i.HandleAsk(ctx, 0, Decision{Action: "ask", TimeoutEnabled: timed, TimeoutSeconds: 1, TimeoutAction: "allow"}, "Write", []byte(`{}`)) {
				t.Fatal("canceled work was allowed")
			}
			cancel()
		}
	}
}
