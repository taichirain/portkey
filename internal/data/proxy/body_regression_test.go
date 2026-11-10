package proxy

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/taichirain/portkey/internal/data/snapshot"
	"github.com/taichirain/portkey/internal/domain/route"
	"github.com/taichirain/portkey/internal/domain/service"
	"go.uber.org/zap"
)

type brokenBody struct{}

func (brokenBody) Read(p []byte) (int, error) {
	copy(p, "partial")
	return 7, errors.New("connection lost")
}
func (brokenBody) Close() error { return nil }

func TestTruncatedRequestBodyNeverReachesUpstream(t *testing.T) {
	var calls atomic.Int32
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer backend.Close()
	s := snapshot.NewConfigSnapshot(uuid.New())
	svc, err := service.New("backend")
	if err != nil {
		t.Fatal(err)
	}
	address, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatal(err)
	}
	host, port, err := net.SplitHostPort(address.Host)
	if err != nil {
		t.Fatal(err)
	}
	svc.Host = host
	svc.Port, err = strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	s.AddService(svc)
	r, err := route.New(svc.ID)
	if err != nil {
		t.Fatal(err)
	}
	r.AddPath("/test")
	r.AddMethod(http.MethodPost)
	s.AddRoute(r)
	if err := s.Build(); err != nil {
		t.Fatal(err)
	}
	p := NewProxy(zap.NewNop())
	p.UpdateSnapshot(s)
	req := httptest.NewRequest(http.MethodPost, "/test", nil)
	req.Body = brokenBody{}
	w := httptest.NewRecorder()
	p.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if calls.Load() != 0 {
		t.Fatal("truncated request reached upstream")
	}
}

func TestLargeResponseThroughProxy(t *testing.T) {
	want := strings.Repeat("0123456789", 20000)
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, want) }))
	defer backend.Close()
	addr, err := url.Parse(backend.URL)
	if err != nil {
		t.Fatal(err)
	}
	host, port, err := net.SplitHostPort(addr.Host)
	if err != nil {
		t.Fatal(err)
	}
	svc, _ := service.New("backend")
	svc.Host = host
	svc.Port, err = strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	s := snapshot.NewConfigSnapshot(uuid.New())
	s.AddService(svc)
	r, _ := route.New(svc.ID)
	r.AddPath("/test")
	r.AddMethod(http.MethodGet)
	s.AddRoute(r)
	if err := s.Build(); err != nil {
		t.Fatal(err)
	}
	p := NewProxy(zap.NewNop())
	p.UpdateSnapshot(s)
	w := httptest.NewRecorder()
	p.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/test", nil))
	if w.Code != http.StatusOK || w.Body.String() != want {
		t.Fatalf("status=%d body length=%d, expected %d", w.Code, w.Body.Len(), len(want))
	}
}
