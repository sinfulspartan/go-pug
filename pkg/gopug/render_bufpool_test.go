package gopug

import (
	"fmt"
	"sync"
	"testing"
)

// TestRenderBufPoolConcurrentDifferentDataPerGoroutine compiles a single
// Template once and renders it from many goroutines simultaneously, each
// goroutine using data unique to itself so its expected output differs from
// every other goroutine's. Template.Render pools its output *bytes.Buffer via
// renderBufPool, and every render Gets, writes, and Puts that buffer back
// during the run, so if a pooled buffer were ever handed to two live renders
// at once (or its contents leaked forward instead of being reset), one
// goroutine's output would show up mixed into another's. Asserting each
// goroutine's own render against its own precomputed answer, repeatedly,
// proves that never happens.
func TestRenderBufPoolConcurrentDifferentDataPerGoroutine(t *testing.T) {
	src := "ul\n  each item in items\n    li: span= item\n"
	tpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	const goroutines = 50
	const rendersEach = 50

	errs := make(chan error, goroutines)
	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			items := make([]any, 0, id+1)
			for i := 0; i <= id; i++ {
				items = append(items, fmt.Sprintf("g%d-item%d", id, i))
			}
			data := map[string]any{"items": items}

			want, err := Render(src, map[string]any{"items": items}, nil)
			if err != nil {
				errs <- fmt.Errorf("goroutine %d baseline Render: %w", id, err)
				return
			}

			for j := 0; j < rendersEach; j++ {
				got, err := tpl.Render(data)
				if err != nil {
					errs <- fmt.Errorf("goroutine %d iteration %d: %w", id, j, err)
					return
				}
				if got != want {
					errs <- fmt.Errorf("goroutine %d iteration %d diverged:\ngot:  %q\nwant: %q", id, j, got, want)
					return
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

// TestRenderBufPoolReusesBuffersAcrossSequentialRenders proves the pooled
// buffer path itself is correct with no concurrency involved: many
// back-to-back renders of the same Template with the same data, each of
// which Gets a buffer from renderBufPool (very likely the very one the
// previous render just Reset and Put back), must all produce byte-identical
// output.
func TestRenderBufPoolReusesBuffersAcrossSequentialRenders(t *testing.T) {
	src := "ul\n  each item in items\n    li: span= item\n"
	data := map[string]any{"items": []any{"a", "b", "c", "d"}}

	tpl, err := Compile(src, nil)
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	want, err := Render(src, data, nil)
	if err != nil {
		t.Fatalf("baseline Render: %v", err)
	}

	for i := 0; i < 200; i++ {
		got, err := tpl.Render(data)
		if err != nil {
			t.Fatalf("Render (iteration %d): %v", i, err)
		}
		if got != want {
			t.Fatalf("Render (iteration %d) = %q, want %q", i, got, want)
		}
	}
}
