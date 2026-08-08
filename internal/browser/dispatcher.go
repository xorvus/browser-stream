package browser

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"
)

var ErrDispatcherClosed = errors.New("input dispatcher is closed")

const (
	moveDeadline   = 250 * time.Millisecond
	urgentDeadline = time.Second
)

type InputSender func(context.Context, Input) error

type Dispatcher struct {
	interval time.Duration
	send     InputSender

	mu        sync.Mutex
	latest    *Input
	closed    bool
	sendGate  chan struct{}
	moveReady chan struct{}
	done      chan struct{}
	closeMu   sync.Once
}

func NewInputDispatcher() *Dispatcher {
	return NewDispatcher(time.Second/60, SendInput)
}

func NewDispatcher(interval time.Duration, send InputSender) *Dispatcher {
	if interval <= 0 {
		interval = time.Second / 60
	}
	d := &Dispatcher{interval: interval, send: send, sendGate: make(chan struct{}, 1), moveReady: make(chan struct{}, 1), done: make(chan struct{})}
	go d.run()
	return d
}

func (d *Dispatcher) Dispatch(ctx context.Context, input Input) error {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return ErrDispatcherClosed
	}
	if input.Type == "move" {
		copy := input
		d.latest = &copy
		d.mu.Unlock()
		select {
		case d.moveReady <- struct{}{}:
		default:
		}
		return nil
	}
	if input.Type == "down" || input.Type == "up" {
		d.latest = nil
	}
	d.mu.Unlock()

	return d.execute(ctx, input, urgentDeadline)
}

func (d *Dispatcher) Close() {
	d.closeMu.Do(func() {
		d.mu.Lock()
		d.closed = true
		d.latest = nil
		d.mu.Unlock()
		close(d.done)
	})
}

func (d *Dispatcher) run() {
	for {
		select {
		case <-d.moveReady:
			d.flushMoves()
		case <-d.done:
			return
		}
	}
}

func (d *Dispatcher) flushMoves() {
	lastMove := time.Time{}
	for {
		if !lastMove.IsZero() {
			wait := d.interval - time.Since(lastMove)
			if wait > 0 {
				timer := time.NewTimer(wait)
				select {
				case <-timer.C:
				case <-d.done:
					if !timer.Stop() {
						select {
						case <-timer.C:
						default:
						}
					}
					return
				}
			}
		}
		d.mu.Lock()
		input := d.latest
		d.latest = nil
		d.mu.Unlock()
		if input == nil {
			return
		}

		if err := d.execute(context.Background(), *input, moveDeadline); err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
			log.Printf("send coalesced pointer input: %v", err)
		}
		lastMove = time.Now()
	}
}

func (d *Dispatcher) execute(parent context.Context, input Input, deadline time.Duration) error {
	ctx, cancel := context.WithTimeout(parent, deadline)
	defer cancel()
	select {
	case d.sendGate <- struct{}{}:
		defer func() { <-d.sendGate }()
	case <-ctx.Done():
		return ctx.Err()
	}
	return d.send(ctx, input)
}
