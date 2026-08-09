package main

import (
	"context"
	"sync"
)

type viewerLifecycle struct {
	mu       sync.Mutex
	viewers  int
	state    string
	setState func(context.Context, string) error
}

func newViewerLifecycle(setState func(context.Context, string) error) *viewerLifecycle {
	return &viewerLifecycle{setState: setState}
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
	if l.viewers == 0 {
		if err := l.setState(ctx, "active"); err != nil {
			return err
		}
		l.state = "active"
	}
	l.viewers++
	return nil
}

func (l *viewerLifecycle) disconnect(ctx context.Context) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.viewers == 0 {
		return
	}
	l.viewers--
	if l.viewers != 0 {
		return
	}
	if err := l.setState(ctx, "frozen"); err == nil {
		l.state = "frozen"
	}
}

func (l *viewerLifecycle) freezeIfIdle(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.viewers != 0 || l.state == "frozen" {
		return nil
	}
	if err := l.setState(ctx, "frozen"); err != nil {
		return err
	}
	l.state = "frozen"
	return nil
}
