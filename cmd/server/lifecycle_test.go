package main

import (
	"context"
	"reflect"
	"testing"
)

func TestViewerLifecycleActivatesFirstAndFreezesLastViewer(t *testing.T) {
	var states []string
	lifecycle := newViewerLifecycle(func(_ context.Context, state string) error {
		states = append(states, state)
		return nil
	})

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

	if !reflect.DeepEqual(states, []string{"frozen", "active", "frozen"}) {
		t.Fatalf("lifecycle states = %#v, want [frozen active frozen]", states)
	}
}

func TestViewerLifecycleDoesNotCountFailedActivation(t *testing.T) {
	var calls int
	lifecycle := newViewerLifecycle(func(_ context.Context, _ string) error {
		calls++
		return context.Canceled
	})

	if err := lifecycle.connect(context.Background()); err == nil {
		t.Fatal("connect succeeded despite failed activation")
	}
	if err := lifecycle.connect(context.Background()); err == nil {
		t.Fatal("second connect succeeded despite failed activation")
	}
	if calls != 2 {
		t.Fatalf("activation calls = %d, want 2", calls)
	}
}
