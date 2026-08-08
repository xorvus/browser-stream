package browser

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestSendInputWithPointerRoutesMovementToPersistentMover(t *testing.T) {
	var gotX, gotY int
	err := sendInputWithPointer(context.Background(), Input{Type: "move", X: 321.9, Y: 654.1}, func(_ context.Context, x, y int) error {
		gotX, gotY = x, y
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotX != 321 || gotY != 654 {
		t.Fatalf("pointer mover received (%d,%d), want (321,654)", gotX, gotY)
	}
}

func TestSendInputWithPointerRejectsUnsupportedInput(t *testing.T) {
	err := sendInputWithPointer(context.Background(), Input{Type: "wheel"}, func(context.Context, int, int) error {
		t.Fatal("unsupported input reached the pointer mover")
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported input type") {
		t.Fatalf("got error %v, want unsupported input type", err)
	}
}

func TestFindVisibleTargetSkipsBlankAndNonPageTargets(t *testing.T) {
	target, err := findVisibleTarget([]debugTarget{
		{Type: "service_worker", URL: "https://example.test/worker"},
		{Type: "page", URL: "about:blank"},
		{ID: "visible", Type: "page", URL: "https://example.test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if target.ID != "visible" {
		t.Fatalf("target ID = %q, want visible", target.ID)
	}
}

func TestCallDevToolsReturnsMatchingCommandResult(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer connection.Close(websocket.StatusNormalClosure, "")
		_, payload, err := connection.Read(r.Context())
		if err != nil {
			return
		}
		var command struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
		}
		if json.Unmarshal(payload, &command) != nil || command.Method != "Runtime.evaluate" {
			return
		}
		response, _ := json.Marshal(map[string]any{"id": command.ID, "result": map[string]string{"value": "selected"}})
		_ = connection.Write(r.Context(), websocket.MessageText, response)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var result struct {
		Value string `json:"value"`
	}
	err := callDevTools(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), "Runtime.evaluate", nil, &result)
	if err != nil {
		t.Fatal(err)
	}
	if result.Value != "selected" {
		t.Fatalf("result value = %q, want selected", result.Value)
	}
}

func TestXdotoolMouseCommandsDoNotWaitForSamePositionMovement(t *testing.T) {
	got := xdotoolMouseArgs(Input{Type: "move", X: 210, Y: 1020})
	want := []string{"mousemove", "210", "1020"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestXdotoolMouseCommandsMoveBeforeButtonChange(t *testing.T) {
	got := xdotoolMouseArgs(Input{Type: "down", X: 25, Y: 30})
	want := []string{"mousemove", "25", "30", "mousedown", "1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestXdotoolMouseArgsBatchMoveAndButtonInOneProcess(t *testing.T) {
	got := xdotoolMouseArgs(Input{Type: "down", X: 25, Y: 30})
	want := []string{"mousemove", "25", "30", "mousedown", "1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want one batched command %#v", got, want)
	}
}

func TestX11KeyMapsControlModifiers(t *testing.T) {
	if got := x11Key(Input{Key: "Control", Code: "ControlLeft"}); got != "Control_L" {
		t.Fatalf("ControlLeft = %q, want Control_L", got)
	}
	if got := x11Key(Input{Key: "Control", Code: "ControlRight"}); got != "Control_R" {
		t.Fatalf("ControlRight = %q, want Control_R", got)
	}
}

func TestBrowserResizeArgsResizeTheVisibleWindow(t *testing.T) {
	got := browserResizeArgs(1280, 720)
	want := []string{"search", "--onlyvisible", "--class", "browser-stream", "windowsize", "%@", "1280", "720"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestInputValidateClampsPointerCoordinates(t *testing.T) {
	got, err := (Input{Type: "move", X: -10, Y: 1200}).Validate(1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	if got.X != 0 || got.Y != 1079 {
		t.Fatalf("got coordinates (%v,%v), want (0,1079)", got.X, got.Y)
	}
}

func TestInputValidateRejectsNonFiniteCoordinates(t *testing.T) {
	for _, coordinate := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		if _, err := (Input{Type: "move", X: coordinate, Y: 1}).Validate(1920, 1080); err == nil {
			t.Fatalf("expected coordinate %v to be rejected", coordinate)
		}
	}
}

func TestInputValidateAcceptsBoundedClipboardText(t *testing.T) {
	got, err := (Input{Type: "paste", Text: "hello"}).Validate(1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	if got.Text != "hello" {
		t.Fatalf("paste text = %q, want hello", got.Text)
	}
}

func TestInputValidateRejectsOversizedOrEmptyClipboardText(t *testing.T) {
	for _, input := range []Input{
		{Type: "paste"},
		{Type: "paste", Text: string(make([]byte, 16*1024+1))},
	} {
		if _, err := input.Validate(1920, 1080); err == nil {
			t.Fatalf("expected %#v to be rejected", input)
		}
	}
}

func TestInputValidateRejectsUnsupportedOrIncompleteInput(t *testing.T) {
	tests := []Input{
		{Type: "wheel"},
		{Type: "keydown", Key: "a"},
		{Type: "keyup", Code: "KeyA"},
		{Type: "keydown", Key: string(make([]byte, 65)), Code: "KeyA"},
	}
	for _, input := range tests {
		if _, err := input.Validate(1920, 1080); err == nil {
			t.Fatalf("expected %#v to be rejected", input)
		}
	}
}
