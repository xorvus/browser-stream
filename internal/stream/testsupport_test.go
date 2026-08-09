package stream

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"
)

type ivfTestFrame struct {
	timestamp uint64
	payload   []byte
}

// ivfStream builds a minimal IVF byte stream shaped exactly like ffmpeg's IVF
// muxer output: denominator=FPS, numerator=1, and raw frame-counter
// timestamps, so a dropped frame appears as a skipped counter value.
func ivfStream(t *testing.T, fps uint32, frames []ivfTestFrame) *bytes.Reader {
	t.Helper()
	buffer := &bytes.Buffer{}
	header := make([]byte, 32)
	copy(header[0:4], "DKIF")
	binary.LittleEndian.PutUint16(header[4:6], 0)
	binary.LittleEndian.PutUint16(header[6:8], 32)
	copy(header[8:12], "VP80")
	binary.LittleEndian.PutUint16(header[12:14], 640)
	binary.LittleEndian.PutUint16(header[14:16], 360)
	binary.LittleEndian.PutUint32(header[16:20], fps) // timebase denominator
	binary.LittleEndian.PutUint32(header[20:24], 1)   // timebase numerator
	binary.LittleEndian.PutUint32(header[24:28], uint32(len(frames)))
	buffer.Write(header)

	for _, frame := range frames {
		frameHeader := make([]byte, 12)
		binary.LittleEndian.PutUint32(frameHeader[0:4], uint32(len(frame.payload)))
		binary.LittleEndian.PutUint64(frameHeader[4:12], frame.timestamp)
		buffer.Write(frameHeader)
		buffer.Write(frame.payload)
	}
	return bytes.NewReader(buffer.Bytes())
}

// closeEnough absorbs the rounding in the float conversion from IVF ticks.
func closeEnough(got, want time.Duration) bool {
	delta := got - want
	if delta < 0 {
		delta = -delta
	}
	return delta < time.Millisecond
}

func eventually(t *testing.T, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal(message)
}
