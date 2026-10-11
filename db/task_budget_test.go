package db

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestBudgetConcurrentAdmissionAndRestartPersistence(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	task, err := d.CreateTask("budget", "local", nil, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer d.DeleteTask(task.ID)
	err = d.SetTaskBudget(t.Context(), TaskBudget{TaskID: task.ID, TokenLimit: 100, ReserveTokens: 20, ToolLimit: 5, ReserveTools: 2})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var calls []int64
	for i := 0; i < 8; i++ {
		wg.Go(func() {
			id, _, e := d.ReserveTaskTokens(context.Background(), task.ID, 30)
			if e != nil && !errors.Is(e, ErrTaskBudget) {
				t.Error(e)
			}
			if e == nil {
				mu.Lock()
				calls = append(calls, id)
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	if len(calls) != 3 {
		t.Fatalf("concurrent reservations admitted %d", len(calls))
	}
	for _, id := range calls {
		if e := d.FinishTaskTokens(t.Context(), id, 10, true); e != nil {
			t.Fatal(e)
		}
	}
	reopened, e := Open(testDSN(t))
	if e != nil {
		t.Fatal(e)
	}
	defer reopened.Close()
	b, e := reopened.TaskBudget(t.Context(), task.ID)
	if e != nil || b.TokensUsed != 30 || b.TokensReserved != 0 {
		t.Fatalf("budget lost %+v %v", b, e)
	}
	// Increasing limits never resets accumulated consumption.
	if e = d.SetTaskBudget(t.Context(), TaskBudget{TaskID: task.ID, TokenLimit: 200, ReserveTokens: 20, ToolLimit: 5, ReserveTools: 2}); e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 3; i++ {
		if _, e = d.ConsumeTaskTool(t.Context(), task.ID, false); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = d.ConsumeTaskTool(t.Context(), task.ID, false); !errors.Is(e, ErrTaskBudget) {
		t.Fatal("normal tools passed reserve threshold")
	}
	if _, e = d.ConsumeTaskTool(t.Context(), task.ID, true); e != nil {
		t.Fatal("settlement denied")
	}
	if _, e = d.ConsumeTaskTool(t.Context(), task.ID, true); e != nil {
		t.Fatal("settlement denied")
	}
	if _, e = d.ConsumeTaskTool(t.Context(), task.ID, true); !errors.Is(e, ErrTaskBudget) {
		t.Fatal("hard tool cap exceeded")
	}
}
