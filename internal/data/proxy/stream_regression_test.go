package proxy

import (
	"bytes"
	"net/http/httptest"
	"testing"
)

func TestResponseWriterStreamsAtThresholdAndFlush(t *testing.T) {
	client := httptest.NewRecorder()
	w := newResponseWriter(client)
	first := bytes.Repeat([]byte("a"), 64*1024)
	if _, err := w.Write(first); err != nil {
		t.Fatal(err)
	}
	if w.streaming || client.Body.Len() != 0 {
		t.Fatal("small response was sent early")
	}
	if _, err := w.Write([]byte("b")); err != nil {
		t.Fatal(err)
	}
	if !w.streaming || !bytes.Equal(client.Body.Bytes(), append(first, 'b')) {
		t.Fatal("stream transition lost or reordered bytes")
	}
	w.Flush()
	if !client.Flushed {
		t.Fatal("flush did not reach client")
	}
}

func TestResponseWriterFlushesSmallEvent(t *testing.T) {
	client := httptest.NewRecorder()
	w := newResponseWriter(client)
	_, _ = w.Write([]byte("data: hi\n\n"))
	w.Flush()
	if !w.streaming || !client.Flushed || client.Body.String() != "data: hi\n\n" {
		t.Fatal("event was not flushed")
	}
}
