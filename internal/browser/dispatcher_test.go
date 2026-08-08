package browser

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestDispatcherCoalescesPointerMovesToLatestPosition(t *testing.T) {
	var mu sync.Mutex
	var sent []Input
	dispatcher := NewDispatcher(10*time.Millisecond, func(_ context.Context, input Input) error {
		mu.Lock()
		sent = append(sent, input)
		mu.Unlock()
		return nil
	})
	defer dispatcher.Close()

	for _, input := range []Input{
		{Type: "move", X: 10, Y: 20},
		{Type: "move", X: 30, Y: 40},
		{Type: "move", X: 50, Y: 60},
	} {
		if err := dispatcher.Dispatch(context.Background(), input); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(30 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 1 || sent[0].X != 50 || sent[0].Y != 60 {
		t.Fatalf("got %#v, want one move to (50,60)", sent)
	}
}

func TestDispatcherPreservesUrgentInputOrder(t *testing.T) {
	var mu sync.Mutex
	var sent []Input
	dispatcher := NewDispatcher(time.Hour, func(_ context.Context, input Input) error {
		mu.Lock()
		sent = append(sent, input)
		mu.Unlock()
		return nil
	})
	defer dispatcher.Close()

	inputs := []Input{
		{Type: "down", X: 100, Y: 200},
		{Type: "up", X: 100, Y: 200},
		{Type: "keydown", Key: "a", Code: "KeyA"},
		{Type: "keyup", Key: "a", Code: "KeyA"},
	}
	for _, input := range inputs {
		if err := dispatcher.Dispatch(context.Background(), input); err != nil {
			t.Fatal(err)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(sent) != len(inputs) {
		t.Fatalf("got %d events, want %d", len(sent), len(inputs))
	}
	for i := range inputs {
		if sent[i].Type != inputs[i].Type {
			t.Fatalf("event %d = %q, want %q", i, sent[i].Type, inputs[i].Type)
		}
	}
}

func TestDispatcherRejectsInputAfterClose(t *testing.T) {
	dispatcher := NewDispatcher(time.Second, func(context.Context, Input) error { return nil })
	dispatcher.Close()
	if err := dispatcher.Dispatch(context.Background(), Input{Type: "move"}); err == nil {
		t.Fatal("expected a closed dispatcher to reject input")
	}
}

func TestDispatcherBlockedMoveCannotHoldKeyboardBeyondMoveDeadline(t *testing.T) {
	moveStarted := make(chan struct{})
	var once sync.Once
	dispatcher := NewDispatcher(time.Millisecond, func(ctx context.Context, input Input) error {
		if input.Type == "move" {
			once.Do(func() { close(moveStarted) })
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	})
	defer dispatcher.Close()

	if err := dispatcher.Dispatch(context.Background(), Input{Type: "move", X: 10, Y: 10}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-moveStarted:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("coalesced move did not start")
	}

	started := time.Now()
	if err := dispatcher.Dispatch(context.Background(), Input{Type: "keydown", Key: "a", Code: "KeyA"}); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed >= 500*time.Millisecond {
		t.Fatalf("keyboard waited %s behind a blocked move, want less than 500ms", elapsed)
	}
}

func TestDispatcherKeyboardDoesNotDiscardPendingPointerMove(t *testing.T) {
	var mu sync.Mutex
	var sent []Input
	dispatcher := NewDispatcher(10*time.Millisecond, func(_ context.Context, input Input) error {
		mu.Lock()
		sent = append(sent, input)
		mu.Unlock()
		return nil
	})
	defer dispatcher.Close()

	if err := dispatcher.Dispatch(context.Background(), Input{Type: "move", X: 40, Y: 50}); err != nil {
		t.Fatal(err)
	}
	if err := dispatcher.Dispatch(context.Background(), Input{Type: "keydown", Key: "a", Code: "KeyA"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 2 {
		t.Fatalf("got %#v, want keyboard event and pending pointer move", sent)
	}
	if sent[0].Type != "keydown" || sent[1].Type != "move" {
		t.Fatalf("got event order %q, %q; want keydown, move", sent[0].Type, sent[1].Type)
	}
}

func TestDispatcherExecutesFirstPointerMoveWithoutWaitingForInterval(t *testing.T) {
	sent := make(chan Input, 1)
	dispatcher := NewDispatcher(time.Hour, func(_ context.Context, input Input) error {
		sent <- input
		return nil
	})
	defer dispatcher.Close()

	if err := dispatcher.Dispatch(context.Background(), Input{Type: "move", X: 40, Y: 50}); err != nil {
		t.Fatal(err)
	}
	select {
	case input := <-sent:
		if input.Type != "move" || input.X != 40 || input.Y != 50 {
			t.Fatalf("got %#v, want immediate move", input)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("first pointer move waited for dispatcher interval")
	}
}
