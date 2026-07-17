package gopug

import (
	"bytes"
	"io"
	"testing"
)

// writeOnlyWriter implements only io.Writer, deliberately not
// io.StringWriter, so StringWriter must fall back to wrapping it.
type writeOnlyWriter struct {
	buf bytes.Buffer
}

func (w *writeOnlyWriter) Write(p []byte) (int, error) {
	return w.buf.Write(p)
}

// TestStringWriterFastPath confirms StringWriter returns a writer that
// already implements io.StringWriter unchanged, with no allocation, and that
// its WriteString call reaches the same underlying buffer.
func TestStringWriterFastPath(t *testing.T) {
	var buf bytes.Buffer
	sw := StringWriter(&buf)

	if _, err := sw.WriteString("hello"); err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	if got := buf.String(); got != "hello" {
		t.Errorf("buf.String() = %q, want %q", got, "hello")
	}

	allocs := testing.AllocsPerRun(100, func() {
		_ = StringWriter(&buf)
	})
	if allocs > 0 {
		t.Errorf("StringWriter(*bytes.Buffer) allocs/run = %v, want 0 (the fast path must return w unchanged)", allocs)
	}
}

// TestStringWriterFallback confirms StringWriter wraps a writer that does
// not implement io.StringWriter, and that the wrapper reproduces
// io.WriteString's own byte-for-byte behavior.
func TestStringWriterFallback(t *testing.T) {
	w := &writeOnlyWriter{}
	sw := StringWriter(w)

	n, err := sw.WriteString("world")
	if err != nil {
		t.Fatalf("WriteString: %v", err)
	}
	if n != len("world") {
		t.Errorf("WriteString returned n = %d, want %d", n, len("world"))
	}
	if got := w.buf.String(); got != "world" {
		t.Errorf("w.buf.String() = %q, want %q", got, "world")
	}
}

// TestStringWriterFallbackMatchesIoWriteString proves the fallback wrapper
// is behaviorally identical to calling io.WriteString directly on the same
// underlying writer, for both the written bytes and the returned count.
func TestStringWriterFallbackMatchesIoWriteString(t *testing.T) {
	a := &writeOnlyWriter{}
	b := &writeOnlyWriter{}

	wantN, wantErr := io.WriteString(a, "byte-identical")
	gotN, gotErr := StringWriter(b).WriteString("byte-identical")

	if gotErr != wantErr {
		t.Fatalf("err = %v, want %v", gotErr, wantErr)
	}
	if gotN != wantN {
		t.Errorf("n = %d, want %d", gotN, wantN)
	}
	if a.buf.String() != b.buf.String() {
		t.Errorf("wrapper wrote %q, io.WriteString wrote %q", b.buf.String(), a.buf.String())
	}
}
