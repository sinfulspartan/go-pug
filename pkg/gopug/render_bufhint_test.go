package gopug

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

// TestRenderSizeHintConcurrentRenderIsByteIdentical compiles a single
// Template once and renders it from many goroutines simultaneously, each
// asserting its result equals a known-correct single-threaded render. The
// adaptive output-buffer pre-sizing keyed on the previous render's length
// (Template.renderSizeHint) only reserves buffer capacity — it never changes
// an emitted byte — so every concurrent render must still match exactly,
// and go test -race must find no data race on the shared hint field.
func TestRenderSizeHintConcurrentRenderIsByteIdentical(t *testing.T) {
	src := strings.Join([]string{
		"ul",
		"  each item in items",
		"    li: span= item",
		"",
	}, "\n")
	tpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	newData := func() map[string]any {
		return map[string]any{"items": []any{"a", "b", "c", "d", "e", "f", "g"}}
	}

	want, err := tpl.Render(newData())
	if err != nil {
		t.Fatalf("Render (single-threaded baseline): %v", err)
	}

	const goroutines = 50
	const rendersEach = 50
	errs := make(chan error, goroutines)
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < rendersEach; j++ {
				got, err := tpl.Render(newData())
				if err != nil {
					errs <- err
					return
				}
				if got != want {
					errs <- fmt.Errorf("concurrent render under adaptive buffer hint diverged:\ngot:  %q\nwant: %q", got, want)
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// TestRenderSizeHintDoesNotAffectOutputAcrossVaryingSizes renders the same
// Template with inputs of very different output sizes back to back, proving
// the size hint learned from a large render never truncates or otherwise
// corrupts a subsequent, much smaller render (and vice versa) — the hint
// only ever changes reserved capacity, never what gets written.
func TestRenderSizeHintDoesNotAffectOutputAcrossVaryingSizes(t *testing.T) {
	src := strings.Join([]string{
		"ul",
		"  each item in items",
		"    li: span= item",
		"",
	}, "\n")
	tpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	small := map[string]any{"items": []any{"x"}}
	large := map[string]any{"items": func() []any {
		items := make([]any, 500)
		for i := range items {
			items[i] = fmt.Sprintf("item-%d", i)
		}
		return items
	}()}

	wantSmall, err := Render(src, small, nil)
	if err != nil {
		t.Fatalf("Render (small, baseline): %v", err)
	}
	wantLarge, err := Render(src, large, nil)
	if err != nil {
		t.Fatalf("Render (large, baseline): %v", err)
	}

	for i := 0; i < 5; i++ {
		gotLarge, err := tpl.Render(large)
		if err != nil {
			t.Fatalf("Render (large, iteration %d): %v", i, err)
		}
		if gotLarge != wantLarge {
			t.Errorf("large render at iteration %d diverged from baseline", i)
		}

		gotSmall, err := tpl.Render(small)
		if err != nil {
			t.Fatalf("Render (small, iteration %d): %v", i, err)
		}
		if gotSmall != wantSmall {
			t.Errorf("small render at iteration %d diverged from baseline", i)
		}
	}
}
