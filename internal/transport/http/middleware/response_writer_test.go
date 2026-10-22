package middleware

import (
	"bufio"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type readerOnly struct{ io.Reader }

type trackingWriter struct {
	*httptest.ResponseRecorder
	readFrom, pushed, hijacked bool
	statuses                   []int
}

func (w *trackingWriter) WriteHeader(status int) {
	w.statuses = append(w.statuses, status)
	w.ResponseRecorder.WriteHeader(status)
}

func (w *trackingWriter) ReadFrom(r io.Reader) (int64, error) {
	w.readFrom = true
	return io.Copy(w.ResponseRecorder, r)
}

func (w *trackingWriter) Push(string, *http.PushOptions) error {
	w.pushed = true
	return nil
}

func (w *trackingWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	w.hijacked = true
	left, right := net.Pipe()
	_ = right.Close()
	return left, bufio.NewReadWriter(bufio.NewReader(left), bufio.NewWriter(left)), nil
}

type plainWriter struct{ header http.Header }

func (w *plainWriter) Header() http.Header         { return w.header }
func (w *plainWriter) WriteHeader(int)             {}
func (w *plainWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestResponseWriterTracksImplicitAndExplicitStatus(t *testing.T) {
	for _, tc := range []struct {
		write func(http.ResponseWriter)
		want  int
	}{
		{func(w http.ResponseWriter) { _, _ = io.WriteString(w, "ok") }, 200},
		{func(w http.ResponseWriter) { w.WriteHeader(201) }, 201},
	} {
		wrapped := newResponseWriter(httptest.NewRecorder())
		tc.write(wrapped)
		if wrapped.status != tc.want {
			t.Fatalf("status=%d, want %d", wrapped.status, tc.want)
		}
	}
}

func TestResponseWriterSupportsFlushAndReadFrom(t *testing.T) {
	recorder := &trackingWriter{ResponseRecorder: httptest.NewRecorder()}
	base := newResponseWriter(recorder)
	wrapped := base.withFlush()
	if _, err := io.Copy(wrapped, readerOnly{strings.NewReader("body")}); err != nil {
		t.Fatal(err)
	}
	wrapped.(http.Flusher).Flush()
	if !recorder.readFrom || !recorder.Flushed || base.status != 200 || recorder.Body.String() != "body" {
		t.Fatalf("readFrom=%v flush=%v status=%d body=%q", recorder.readFrom, recorder.Flushed, base.status, recorder.Body.String())
	}
}

func TestResponseWriterPreservesInterimStatus(t *testing.T) {
	base := &trackingWriter{ResponseRecorder: httptest.NewRecorder()}
	w := newResponseWriter(base)
	w.WriteHeader(103)
	w.WriteHeader(201)
	if w.status != 201 || len(base.statuses) != 2 || base.statuses[0] != 103 || base.statuses[1] != 201 {
		t.Fatalf("final=%d statuses=%v", w.status, base.statuses)
	}
}

func TestResponseControllerReportsUnsupportedFlush(t *testing.T) {
	w := newResponseWriter(&plainWriter{header: make(http.Header)})
	if _, ok := any(w.withFlush()).(http.Flusher); ok {
		t.Fatal("wrapper advertises Flush although the original writer cannot flush")
	}
	if err := http.NewResponseController(w).Flush(); !errors.Is(err, http.ErrNotSupported) {
		t.Fatalf("Flush error=%v", err)
	}
}

func TestResponseWriterDelegatesHijackAndPush(t *testing.T) {
	base := &trackingWriter{ResponseRecorder: httptest.NewRecorder()}
	w := newResponseWriter(base)
	if err := w.Push("/resource", nil); err != nil {
		t.Fatal(err)
	}
	conn, _, err := w.Hijack()
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	if !base.pushed || !base.hijacked {
		t.Fatalf("push=%v hijack=%v", base.pushed, base.hijacked)
	}
}
