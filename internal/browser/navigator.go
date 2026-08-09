package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/coder/websocket"
)

type debugTarget struct {
	ID           string `json:"id"`
	Type         string `json:"type"`
	URL          string `json:"url"`
	WebSocketURL string `json:"webSocketDebuggerUrl"`
}

type Input struct {
	Type string  `json:"type"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
	Key  string  `json:"key"`
	Code string  `json:"code"`
	Text string  `json:"text"`
}

const maxClipboardTextBytes = 16 * 1024

const debugTargetsURL = "http://127.0.0.1:9222/json/list"

const pageLifecycleTimeout = 5 * time.Second

var defaultPointerMover = newX11PointerMover()

func (input Input) Validate(width, height int) (Input, error) {
	switch input.Type {
	case "move", "down", "up":
		if width <= 0 || height <= 0 {
			return Input{}, fmt.Errorf("invalid display dimensions")
		}
		if math.IsNaN(input.X) || math.IsInf(input.X, 0) || math.IsNaN(input.Y) || math.IsInf(input.Y, 0) {
			return Input{}, fmt.Errorf("pointer coordinates must be finite")
		}
		input.X = min(max(input.X, 0), float64(width-1))
		input.Y = min(max(input.Y, 0), float64(height-1))
	case "keydown", "keyup":
		if input.Key == "" || input.Code == "" {
			return Input{}, fmt.Errorf("keyboard input requires key and code")
		}
		if len(input.Key) > 64 || len(input.Code) > 64 {
			return Input{}, fmt.Errorf("keyboard input is too long")
		}
	case "paste":
		if input.Text == "" || len(input.Text) > maxClipboardTextBytes {
			return Input{}, fmt.Errorf("clipboard text must be between 1 and %d bytes", maxClipboardTextBytes)
		}
	default:
		return Input{}, fmt.Errorf("unsupported input type")
	}
	return input, nil
}

func SendInput(parent context.Context, input Input) error {
	return sendInputWithPointer(parent, input, defaultPointerMover.Move)
}

func sendInputWithPointer(parent context.Context, input Input, move pointerMover) error {
	switch input.Type {
	case "move":
		return move(parent, int(input.X), int(input.Y))
	case "down", "up":
		return x11Mouse(parent, input)
	case "keydown":
		return exec.CommandContext(parent, "xdotool", "keydown", x11Key(input)).Run()
	case "keyup":
		return exec.CommandContext(parent, "xdotool", "keyup", x11Key(input)).Run()
	case "paste":
		return exec.CommandContext(parent, "xdotool", "type", "--clearmodifiers", "--delay", "0", "--", input.Text).Run()
	default:
		return fmt.Errorf("unsupported input type")
	}
}

func x11Key(input Input) string {
	if strings.HasPrefix(input.Code, "Key") {
		return strings.ToLower(strings.TrimPrefix(input.Code, "Key"))
	}
	if strings.HasPrefix(input.Code, "Digit") {
		return strings.TrimPrefix(input.Code, "Digit")
	}
	switch input.Code {
	case "ControlLeft":
		return "Control_L"
	case "ControlRight":
		return "Control_R"
	case "Space":
		return "space"
	case "Enter":
		return "Return"
	case "Backspace":
		return "BackSpace"
	case "Tab":
		return "Tab"
	case "Escape":
		return "Escape"
	case "ArrowLeft":
		return "Left"
	case "ArrowRight":
		return "Right"
	case "ArrowUp":
		return "Up"
	case "ArrowDown":
		return "Down"
	case "Semicolon":
		return "semicolon"
	case "Comma":
		return "comma"
	case "Period":
		return "period"
	case "Slash":
		return "slash"
	case "Quote":
		return "apostrophe"
	case "BracketLeft":
		return "bracketleft"
	case "BracketRight":
		return "bracketright"
	case "Minus":
		return "minus"
	case "Equal":
		return "equal"
	}
	return input.Key
}

func x11Mouse(ctx context.Context, input Input) error {
	return exec.CommandContext(ctx, "xdotool", xdotoolMouseArgs(input)...).Run()
}

func xdotoolMouseArgs(input Input) []string {
	x, y := strconv.Itoa(int(input.X)), strconv.Itoa(int(input.Y))
	args := []string{"mousemove", x, y}
	if input.Type == "down" {
		args = append(args, "mousedown", "1")
	}
	if input.Type == "up" {
		args = append(args, "mouseup", "1")
	}
	return args
}

func ResizeViewport(ctx context.Context, width, height int) error {
	if width <= 0 || height <= 0 {
		return fmt.Errorf("viewport dimensions must be positive")
	}
	return exec.CommandContext(ctx, "xdotool", browserResizeArgs(width, height)...).Run()
}

func browserResizeArgs(width, height int) []string {
	return []string{
		"search", "--onlyvisible", "--class", "browser-stream",
		"windowsize", "%@", strconv.Itoa(width), strconv.Itoa(height),
	}
}

func Navigate(parent context.Context, rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", fmt.Errorf("URL must be an absolute HTTP(S) URL")
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()

	target, err := visibleTarget(ctx)
	if err != nil {
		return "", err
	}
	if err := navigateTarget(ctx, target.WebSocketURL, parsed.String()); err != nil {
		return "", err
	}
	return parsed.String(), nil
}

func SetPageLifecycleState(parent context.Context, state string) error {
	if state != "active" && state != "frozen" {
		return fmt.Errorf("unsupported page lifecycle state %q", state)
	}
	ctx, cancel := context.WithTimeout(parent, pageLifecycleTimeout)
	defer cancel()
	target, err := visibleTarget(ctx)
	if err != nil {
		return err
	}
	return setPageLifecycleStateTarget(ctx, target.WebSocketURL, state)
}

func SelectedText(parent context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	target, err := visibleTarget(ctx)
	if err != nil {
		return "", err
	}
	return selectedTextTarget(ctx, target.WebSocketURL)
}

func visibleTarget(ctx context.Context) (debugTarget, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, debugTargetsURL, nil)
	if err != nil {
		return debugTarget{}, err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return debugTarget{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return debugTarget{}, fmt.Errorf("list Chromium pages: %s", response.Status)
	}
	var targets []debugTarget
	if err := json.NewDecoder(response.Body).Decode(&targets); err != nil {
		return debugTarget{}, err
	}
	return findVisibleTarget(targets)
}

func findVisibleTarget(targets []debugTarget) (debugTarget, error) {
	for _, target := range targets {
		if target.Type == "page" && target.URL != "about:blank" {
			return target, nil
		}
	}
	return debugTarget{}, fmt.Errorf("no visible Chromium page found")
}

func selectedTextTarget(ctx context.Context, endpoint string) (string, error) {
	var result struct {
		Result struct {
			Value string `json:"value"`
		} `json:"result"`
	}
	err := callDevTools(ctx, endpoint, "Runtime.evaluate", map[string]any{
		"expression":    "window.getSelection().toString()",
		"returnByValue": true,
	}, &result)
	if err != nil {
		return "", err
	}
	if len(result.Result.Value) > maxClipboardTextBytes {
		return "", fmt.Errorf("selected text exceeds %d bytes", maxClipboardTextBytes)
	}
	return result.Result.Value, nil
}

func navigateTarget(ctx context.Context, endpoint, rawURL string) error {
	return callDevTools(ctx, endpoint, "Page.navigate", map[string]string{"url": rawURL}, nil)
}

func setPageLifecycleStateTarget(ctx context.Context, endpoint, state string) error {
	return callDevTools(ctx, endpoint, "Page.setWebLifecycleState", map[string]string{"state": state}, nil)
}

func callDevTools(ctx context.Context, endpoint, method string, params, result any) error {
	connection, _, err := websocket.Dial(ctx, endpoint, nil)
	if err != nil {
		return err
	}
	defer connection.Close(websocket.StatusNormalClosure, "")
	message, err := json.Marshal(map[string]any{"id": 1, "method": method, "params": params})
	if err != nil {
		return err
	}
	if err := connection.Write(ctx, websocket.MessageText, message); err != nil {
		return err
	}
	for {
		_, response, err := connection.Read(ctx)
		if err != nil {
			return err
		}
		var responseMessage struct {
			ID     int             `json:"id"`
			Error  json.RawMessage `json:"error"`
			Result json.RawMessage `json:"result"`
		}
		if err := json.Unmarshal(response, &responseMessage); err != nil {
			return err
		}
		if responseMessage.ID != 1 {
			continue
		}
		if len(responseMessage.Error) > 0 {
			return fmt.Errorf("%s failed: %s", method, responseMessage.Error)
		}
		if result == nil || len(responseMessage.Result) == 0 {
			return nil
		}
		return json.Unmarshal(responseMessage.Result, result)
	}
}
