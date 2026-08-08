package browser

import (
	"context"
	"fmt"
	"math"
	"sync"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

type pointerMover func(context.Context, int, int) error

type x11PointerMover struct {
	once sync.Once
	conn *xgb.Conn
	root xproto.Window
	err  error
}

func newX11PointerMover() *x11PointerMover {
	return &x11PointerMover{}
}

func (m *x11PointerMover) Move(ctx context.Context, x, y int) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	m.once.Do(m.connect)
	if m.err != nil {
		return m.err
	}
	if x < math.MinInt16 || x > math.MaxInt16 || y < math.MinInt16 || y > math.MaxInt16 {
		return fmt.Errorf("pointer coordinate outside X11 range")
	}
	xproto.WarpPointer(m.conn, xproto.WindowNone, m.root, 0, 0, 0, 0, int16(x), int16(y))
	return nil
}

func (m *x11PointerMover) connect() {
	m.conn, m.err = xgb.NewConn()
	if m.err != nil {
		m.err = fmt.Errorf("connect persistent X11 pointer mover: %w", m.err)
		return
	}
	screen := xproto.Setup(m.conn).DefaultScreen(m.conn)
	if screen == nil {
		m.conn.Close()
		m.conn = nil
		m.err = fmt.Errorf("X11 default screen is unavailable")
		return
	}
	m.root = screen.Root
}
