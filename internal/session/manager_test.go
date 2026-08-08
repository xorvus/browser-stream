package session

import "testing"

func TestManagerRejectsSecondActiveViewer(t *testing.T) {
	var manager Manager
	release, ok := manager.Acquire()
	if !ok {
		t.Fatal("first viewer was rejected")
	}

	if _, ok := manager.Acquire(); ok {
		t.Fatal("second viewer was accepted")
	}

	release()
	if _, ok := manager.Acquire(); !ok {
		t.Fatal("viewer slot was not released")
	}
}

func TestManagerReleaseIsIdempotent(t *testing.T) {
	var manager Manager
	release, ok := manager.Acquire()
	if !ok {
		t.Fatal("first viewer was rejected")
	}

	release()
	release()
	if _, ok := manager.Acquire(); !ok {
		t.Fatal("viewer slot was not released")
	}
}
