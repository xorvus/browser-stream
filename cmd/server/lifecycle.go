package main

import (
	"context"
	"sync"
	"time"
)

const (
	// idleFreezeDelay is how long the last viewer has to come back before the
	// browser page is frozen.
	//
	// Freezing immediately made a viewer reloading the page pay for it: the
	// tab drops to zero viewers for a few hundred milliseconds, which froze the
	// remote page and then unfroze it a moment later. A page coming out of the
	// frozen state does not always repaint, so the stream showed a blank white
	// frame until something forced the compositor to produce a new one.
	//
	// The encoder still stops the instant the last viewer leaves, so an idle
	// server costs nothing during this window; only the page's own scripts keep
	// running.
	idleFreezeDelay = 15 * time.Second

	// freezeTimeout bounds the CDP call made from the idle timer, which has no
	// request context of its own.
	freezeTimeout = 10 * time.Second
)

type viewerLifecycle struct {
	mu        sync.Mutex
	viewers   int
	state     string
	setState  func(context.Context, string) error
	idleDelay time.Duration
	idleTimer *time.Timer
}

func newViewerLifecycle(setState func(context.Context, string) error) *viewerLifecycle {
	return newViewerLifecycleWithDelay(setState, idleFreezeDelay)
}

func newViewerLifecycleWithDelay(setState func(context.Context, string) error, idleDelay time.Duration) *viewerLifecycle {
	return &viewerLifecycle{setState: setState, idleDelay: idleDelay}
}

type viewerLifecyclePort interface {
	connect(context.Context) error
	disconnect(context.Context)
}

type noopViewerLifecycle struct{}

func (noopViewerLifecycle) connect(context.Context) error { return nil }
func (noopViewerLifecycle) disconnect(context.Context)    {}

func (l *viewerLifecycle) connect(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cancelIdleLocked()
	if l.viewers == 0 {
		if err := l.setState(ctx, "active"); err != nil {
			return err
		}
		l.state = "active"
	}
	l.viewers++
	return nil
}

func (l *viewerLifecycle) disconnect(context.Context) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.viewers == 0 {
		return
	}
	l.viewers--
	if l.viewers != 0 {
		return
	}
	if l.idleDelay <= 0 {
		l.freezeLocked(context.Background())
		return
	}
	// The departing viewer's request context is already cancelled by the time
	// this runs, so the timer uses its own.
	l.cancelIdleLocked()
	l.idleTimer = time.AfterFunc(l.idleDelay, l.freezeIfStillIdle)
}

func (l *viewerLifecycle) freezeIfStillIdle() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.idleTimer = nil
	if l.viewers != 0 {
		return
	}
	l.freezeLocked(context.Background())
}

func (l *viewerLifecycle) freezeIfIdle(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.viewers != 0 || l.state == "" || l.state == "frozen" {
		return nil
	}
	if err := l.setState(ctx, "frozen"); err != nil {
		return err
	}
	l.state = "frozen"
	return nil
}

func (l *viewerLifecycle) freezeLocked(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, freezeTimeout)
	defer cancel()
	if err := l.setState(ctx, "frozen"); err == nil {
		l.state = "frozen"
	}
}

func (l *viewerLifecycle) cancelIdleLocked() {
	if l.idleTimer == nil {
		return
	}
	l.idleTimer.Stop()
	l.idleTimer = nil
}
