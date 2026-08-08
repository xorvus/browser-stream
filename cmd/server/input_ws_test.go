package main

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"browser-stream/internal/browser"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

type recordedInput struct {
	input browser.Input
	at    time.Time
}

type inputRecorder struct {
	mu     sync.Mutex
	events []recordedInput
	notify chan struct{}
}

func newInputRecorder() *inputRecorder {
	return &inputRecorder{notify: make(chan struct{}, 32)}
}

func (r *inputRecorder) dispatch(_ context.Context, input browser.Input) error {
	r.mu.Lock()
	r.events = append(r.events, recordedInput{input: input, at: time.Now()})
	r.mu.Unlock()
	select {
	case r.notify <- struct{}{}:
	default:
	}
	return nil
}

func (r *inputRecorder) snapshot() []recordedInput {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recordedInput(nil), r.events...)
}

func dialInputTestServer(t *testing.T, recorder *inputRecorder) (*websocket.Conn, context.Context) {
	t.Helper()
	server := httptest.NewServer(newInputWebSocketHandler(1920, 1080, recorder.dispatch))
	t.Cleanup(server.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	t.Cleanup(cancel)
	connection, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { connection.CloseNow() })
	return connection, ctx
}

func TestInputWebSocketPreservesUrgentOrderAndAcknowledgesExecution(t *testing.T) {
	recorder := newInputRecorder()
	connection, ctx := dialInputTestServer(t, recorder)

	messages := []inputWireMessage{
		{Version: 1, Sequence: 10, Input: browser.Input{Type: "keydown", Key: "a", Code: "KeyA"}},
		{Version: 1, Sequence: 11, Input: browser.Input{Type: "keyup", Key: "a", Code: "KeyA"}},
	}
	for _, message := range messages {
		if err := wsjson.Write(ctx, connection, message); err != nil {
			t.Fatal(err)
		}
	}
	for _, sequence := range []uint64{10, 11} {
		var acknowledgement inputAcknowledgement
		if err := wsjson.Read(ctx, connection, &acknowledgement); err != nil {
			t.Fatal(err)
		}
		if acknowledgement.Ack != sequence || acknowledgement.Error != "" {
			t.Fatalf("got acknowledgement %#v, want successful ack %d", acknowledgement, sequence)
		}
	}

	events := recorder.snapshot()
	if len(events) != 2 || events[0].input.Type != "keydown" || events[1].input.Type != "keyup" {
		t.Fatalf("got events %#v, want ordered keydown and keyup", events)
	}
}

func TestInputWebSocketRejectsInvalidInputWithoutDispatching(t *testing.T) {
	recorder := newInputRecorder()
	connection, ctx := dialInputTestServer(t, recorder)

	message := inputWireMessage{Version: 1, Sequence: 7, Input: browser.Input{Type: "wheel"}}
	if err := wsjson.Write(ctx, connection, message); err != nil {
		t.Fatal(err)
	}
	var acknowledgement inputAcknowledgement
	if err := wsjson.Read(ctx, connection, &acknowledgement); err != nil {
		t.Fatal(err)
	}
	if acknowledgement.Ack != 7 || acknowledgement.Error == "" {
		t.Fatalf("got acknowledgement %#v, want validation error for sequence 7", acknowledgement)
	}
	if got := recorder.snapshot(); len(got) != 0 {
		t.Fatalf("invalid input was dispatched: %#v", got)
	}
}

func TestInputWebSocketReleasesHeldInputOnDisconnect(t *testing.T) {
	recorder := newInputRecorder()
	connection, ctx := dialInputTestServer(t, recorder)

	for _, message := range []inputWireMessage{
		{Version: 1, Sequence: 1, Input: browser.Input{Type: "keydown", Key: "Shift", Code: "ShiftLeft"}},
		{Version: 1, Sequence: 2, Input: browser.Input{Type: "down", X: 50, Y: 60}},
	} {
		if err := wsjson.Write(ctx, connection, message); err != nil {
			t.Fatal(err)
		}
		var acknowledgement inputAcknowledgement
		if err := wsjson.Read(ctx, connection, &acknowledgement); err != nil {
			t.Fatal(err)
		}
	}
	if err := connection.Close(websocket.StatusNormalClosure, "test complete"); err != nil {
		t.Fatal(err)
	}

	deadline := time.After(time.Second)
	for len(recorder.snapshot()) < 4 {
		select {
		case <-recorder.notify:
		case <-deadline:
			t.Fatalf("timed out waiting for disconnect releases: %#v", recorder.snapshot())
		}
	}
	events := recorder.snapshot()
	if events[2].input.Type != "keyup" || events[2].input.Code != "ShiftLeft" || events[3].input.Type != "up" {
		t.Fatalf("got disconnect events %#v, want keyup then mouse up", events[2:])
	}
}

func TestInputWireMessageUsesFlatJSONShape(t *testing.T) {
	message := inputWireMessage{Version: 1, Sequence: 3, Input: browser.Input{Type: "move", X: 10, Y: 20}}
	encoded, err := json.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), `"Input"`) || !strings.Contains(string(encoded), `"type":"move"`) {
		t.Fatalf("unexpected wire format: %s", encoded)
	}
}
