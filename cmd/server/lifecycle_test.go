package main

import (
	"context"
	"reflect"
	"sync"
	"testing"
	"time"
)

type stateRecorder struct {
	mu     sync.Mutex
	states []string
	err    error
}

func (r *stateRecorder) set(_ context.Context, state string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.states = append(r.states, state)
	return nil
}

func (r *stateRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.states...)
}

func TestViewerLifecycleActivatesFirstAndFreezesLastViewer(t *testing.T) {
	recorder := &stateRecorder{}
	lifecycle := newViewerLifecycleWithDelay(recorder.set, 0)

	if err := lifecycle.freezeIfIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := lifecycle.connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	lifecycle.disconnect(context.Background())
	lifecycle.disconnect(context.Background())

	if got := recorder.snapshot(); !reflect.DeepEqual(got, []string{"active", "frozen"}) {
		t.Fatalf("lifecycle states = %#v, want [active frozen]", got)
	}
}

func TestViewerLifecycleDoesNotFreezeWhileAViewerReconnects(t *testing.T) {
	// Reloading the viewer page drops to zero viewers for a moment. Freezing
	// and unfreezing the browser page across that gap left it showing a blank
	// white frame, so a reconnect inside the idle delay must cancel the freeze
	// entirely rather than undo it. Re-activating is fine and cheap; freezing
	// is what has to be avoided.
	recorder := &stateRecorder{}
	lifecycle := newViewerLifecycleWithDelay(recorder.set, 50*time.Millisecond)

	if err := lifecycle.connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	lifecycle.disconnect(context.Background())
	if err := lifecycle.connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	time.Sleep(120 * time.Millisecond)

	for _, state := range recorder.snapshot() {
		if state == "frozen" {
			t.Fatalf("lifecycle states = %#v, want no freeze across a viewer reload", recorder.snapshot())
		}
	}
}

func TestViewerLifecycleFreezesAfterTheIdleDelay(t *testing.T) {
	recorder := &stateRecorder{}
	lifecycle := newViewerLifecycleWithDelay(recorder.set, 20*time.Millisecond)

	if err := lifecycle.connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	lifecycle.disconnect(context.Background())

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if reflect.DeepEqual(recorder.snapshot(), []string{"active", "frozen"}) {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("lifecycle states = %#v, want the page frozen once the viewer stayed away", recorder.snapshot())
}

func TestViewerLifecycleReactivatesAfterAnIdleFreeze(t *testing.T) {
	recorder := &stateRecorder{}
	lifecycle := newViewerLifecycleWithDelay(recorder.set, 0)

	if err := lifecycle.connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	lifecycle.disconnect(context.Background())
	if err := lifecycle.connect(context.Background()); err != nil {
		t.Fatal(err)
	}

	if got := recorder.snapshot(); !reflect.DeepEqual(got, []string{"active", "frozen", "active"}) {
		t.Fatalf("lifecycle states = %#v, want the page reactivated for the returning viewer", got)
	}
}

func TestViewerLifecycleDoesNotCountFailedActivation(t *testing.T) {
	recorder := &stateRecorder{err: context.Canceled}
	lifecycle := newViewerLifecycleWithDelay(recorder.set, 0)

	if err := lifecycle.connect(context.Background()); err == nil {
		t.Fatal("connect succeeded despite failed activation")
	}
	if err := lifecycle.connect(context.Background()); err == nil {
		t.Fatal("second connect succeeded despite failed activation")
	}
}
