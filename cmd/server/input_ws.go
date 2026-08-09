package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"browser-stream/internal/browser"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

const (
	inputProtocolVersion = 1
	inputReadLimit       = 4 * 1024
	inputWriteTimeout    = time.Second
)

type inputWireMessage struct {
	Version  int    `json:"v"`
	Sequence uint64 `json:"seq"`
	browser.Input
}

type inputAcknowledgement struct {
	Version int    `json:"v"`
	Ack     uint64 `json:"ack"`
	Error   string `json:"error,omitempty"`
}

type inputDispatch func(context.Context, browser.Input) error

type inputSession struct {
	dispatch inputDispatch
	width    int
	height   int
	keys     map[string]browser.Input
	button   *browser.Input
	lastX    float64
	lastY    float64
}

func newInputWebSocketHandler(width, height int, dispatch inputDispatch) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer connection.CloseNow()
		connection.SetReadLimit(inputReadLimit)

		session := &inputSession{
			dispatch: dispatch,
			width:    width,
			height:   height,
			keys:     make(map[string]browser.Input),
		}
		defer session.releaseAll()
		ctx := r.Context()
		for {
			var message inputWireMessage
			if err := wsjson.Read(ctx, connection, &message); err != nil {
				return
			}
			err := session.handle(ctx, message)
			if message.Type == "move" && err == nil {
				continue
			}
			ack := inputAcknowledgement{Version: inputProtocolVersion, Ack: message.Sequence}
			if err != nil {
				ack.Error = err.Error()
			}
			writeCtx, cancel := context.WithTimeout(ctx, inputWriteTimeout)
			err = wsjson.Write(writeCtx, connection, ack)
			cancel()
			if err != nil {
				return
			}
		}
	})
}

func (s *inputSession) handle(ctx context.Context, message inputWireMessage) error {
	if message.Version != inputProtocolVersion {
		return fmt.Errorf("unsupported input protocol version")
	}
	if message.Sequence == 0 {
		return fmt.Errorf("input sequence must be positive")
	}
	input, err := message.Input.Validate(s.width, s.height)
	if err != nil {
		return err
	}

	switch input.Type {
	case "keydown":
		if _, exists := s.keys[input.Code]; exists {
			return nil
		}
	case "keyup":
		if _, exists := s.keys[input.Code]; !exists {
			return nil
		}
	case "down":
		if s.button != nil {
			return nil
		}
	case "up":
		if s.button == nil {
			return nil
		}
	}

	if err := s.dispatch(ctx, input); err != nil {
		return fmt.Errorf("execute input: %w", err)
	}
	s.lastX, s.lastY = input.X, input.Y
	switch input.Type {
	case "keydown":
		s.keys[input.Code] = input
	case "keyup":
		delete(s.keys, input.Code)
	case "down":
		copy := input
		s.button = &copy
	case "up":
		s.button = nil
	}
	return nil
}

func (s *inputSession) releaseAll() {
	for _, input := range s.keys {
		ctx, cancel := context.WithTimeout(context.Background(), inputWriteTimeout)
		_ = s.dispatch(ctx, browser.Input{Type: "keyup", Key: input.Key, Code: input.Code})
		cancel()
	}
	if s.button != nil {
		ctx, cancel := context.WithTimeout(context.Background(), inputWriteTimeout)
		_ = s.dispatch(ctx, browser.Input{Type: "up", X: s.lastX, Y: s.lastY})
		cancel()
	}
}
