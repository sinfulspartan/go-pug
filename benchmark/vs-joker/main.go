// Command vs-joker is a standalone, three-way render-throughput comparison
// between go-pug and Joker/jade (github.com/Joker/jade), a mature,
// independently developed Pug/Jade engine for Go. It measures:
//
//   - the go-pug interpreter (gopug.Template.Render)
//   - go-pug's typed codegen (a generated Go render function, built once via
//     gengopug and called directly — see codegen_*.go)
//   - Joker/jade's precompiled path (a generated Go render function, built
//     once via the jade CLI and called directly — see joker_*.go)
//
// against the same four templates as ../ (the main 3-way corpus): card_list,
// table, form, blog. It is a separate module (see go.mod) specifically so
// the root go-pug module never gains a dependency on Joker/jade — see
// README.md for the full methodology, fairness caveats, and results.
//
// Usage, from benchmark/vs-joker:
//
//	go run .
package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/sinfulspartan/go-pug/pkg/gopug"
)

// The timing scheme mirrors ../main.go's measureRendersPerSec, with one
// deliberate difference explained below: a calibration run estimates
// renders/second, which sizes a per-repetition iteration count aimed at
// targetRepSeconds of wall time (clamped to [minIterations, maxIterations]);
// each of repetitions repetitions discards a warmupFraction-sized warmup
// before its own timed portion; the reported figure is the median
// renders/second (and ns/op) across the repetitions.
//
// The difference: ../main.go's calibration always times a fixed
// calibrationIterations (30) render calls. That is safe for the slower
// engines it measures, but go-pug codegen and Joker's precompiled path both
// render some of these templates in low single-digit microseconds, and 30 of
// those can complete faster than this machine's timer resolution reliably
// distinguishes — occasionally measuring as an exact zero duration, which
// would make the calibration estimate a wildly inflated rate (and then a
// wildly inflated, minutes-long iteration count). calibrateRate below
// doubles the iteration count until the batch takes at least
// calibrationMinDuration to render, which is immune to that failure mode
// regardless of how fast the engine is.
const (
	calibrationMinDuration = 20 * time.Millisecond
	targetRepSeconds       = 0.4
	minIterations          = 200
	maxIterations          = 3_000_000
	warmupFraction         = 0.15
	repetitions            = 5

	allocSampleRuns = 500
)

func medianOf5(v [repetitions]float64) float64 {
	s := v[:]
	sort.Float64s(s)
	return s[len(s)/2]
}

// calibrateRate returns an estimated renders/second for render, timing
// batches that double in size until one takes at least
// calibrationMinDuration — see the package-level doc comment above for why
// a fixed iteration count isn't reliable here.
func calibrateRate(render func()) float64 {
	for iters := 8; ; iters *= 2 {
		start := time.Now()
		for i := 0; i < iters; i++ {
			render()
		}
		el := time.Since(start)
		if el >= calibrationMinDuration || iters >= (1<<30) {
			return float64(iters) / el.Seconds()
		}
	}
}

// measure times only render (calibrate/warm-up/repeat/median, see the
// package doc above) and separately samples allocations per call via
// testing.AllocsPerRun, which is a real, public API usable outside of `go
// test` — it disables the GC around a batch of allocSampleRuns calls and
// reports the average allocations per call.
func measure(render func()) (nsPerOp, allocsPerOp, rendersPerSec float64) {
	rate := calibrateRate(render)

	n := int(rate * targetRepSeconds)
	if n < minIterations {
		n = minIterations
	}
	if n > maxIterations {
		n = maxIterations
	}
	warmup := int(float64(n) * warmupFraction)
	if warmup < 1 {
		warmup = 1
	}

	var repRates [repetitions]float64
	var repNs [repetitions]float64
	for r := 0; r < repetitions; r++ {
		for i := 0; i < warmup; i++ {
			render()
		}
		start := time.Now()
		for i := 0; i < n; i++ {
			render()
		}
		el := time.Since(start).Seconds()
		if el <= 0 {
			el = 1e-9
		}
		repRates[r] = float64(n) / el
		repNs[r] = el * 1e9 / float64(n)
	}

	allocsPerOp = testing.AllocsPerRun(allocSampleRuns, render)
	return medianOf5(repNs), allocsPerOp, medianOf5(repRates)
}

type engineResult struct {
	html          string
	nsPerOp       float64
	allocsPerOp   float64
	rendersPerSec float64
}

// stripVoidSelfClose normalizes Joker's self-closed void elements
// (`<input .../>`) to go-pug/pugjs's terse HTML5 form (`<input ...>`) so the
// byte-identity check can report whether a template is identical for a
// reason unrelated to this known, disclosed divergence. See README.md's
// "Output fidelity" section — form is the only template in this corpus
// where this normalization is needed.
func stripVoidSelfClose(s string) string {
	return strings.ReplaceAll(s, "\"/>", "\">")
}

func trimTrailingNewline(s string) string {
	return strings.TrimSuffix(s, "\n")
}

type templateCase struct {
	name          string
	pugFile       string
	interpData    map[string]any
	renderInterp  func(tpl *gopug.Template) engineResult
	renderCodegen func(buf *bytes.Buffer) engineResult
	renderJoker   func(buf *bytes.Buffer) engineResult
}

func cases() []templateCase {
	return []templateCase{
		{
			name:       "card_list",
			pugFile:    "card_list.pug",
			interpData: cardListMap(),
			renderCodegen: func(buf *bytes.Buffer) engineResult {
				d := cardListFixture()
				render := func() {
					buf.Reset()
					if err := CGCardList(buf, d); err != nil {
						panic(err)
					}
				}
				ns, allocs, rps := measure(render)
				buf.Reset()
				_ = CGCardList(buf, d)
				return engineResult{html: buf.String(), nsPerOp: ns, allocsPerOp: allocs, rendersPerSec: rps}
			},
			renderJoker: func(buf *bytes.Buffer) engineResult {
				d := cardListFixture()
				render := func() {
					buf.Reset()
					CardList(d, buf)
				}
				ns, allocs, rps := measure(render)
				buf.Reset()
				CardList(d, buf)
				return engineResult{html: buf.String(), nsPerOp: ns, allocsPerOp: allocs, rendersPerSec: rps}
			},
		},
		{
			name:       "table",
			pugFile:    "table.pug",
			interpData: tableMap(),
			renderCodegen: func(buf *bytes.Buffer) engineResult {
				d := tableFixture()
				render := func() {
					buf.Reset()
					if err := CGTable(buf, d); err != nil {
						panic(err)
					}
				}
				ns, allocs, rps := measure(render)
				buf.Reset()
				_ = CGTable(buf, d)
				return engineResult{html: buf.String(), nsPerOp: ns, allocsPerOp: allocs, rendersPerSec: rps}
			},
			renderJoker: func(buf *bytes.Buffer) engineResult {
				d := tableFixture()
				render := func() {
					buf.Reset()
					Table(d, buf)
				}
				ns, allocs, rps := measure(render)
				buf.Reset()
				Table(d, buf)
				return engineResult{html: buf.String(), nsPerOp: ns, allocsPerOp: allocs, rendersPerSec: rps}
			},
		},
		{
			name:       "form",
			pugFile:    "form.pug",
			interpData: formMap(),
			renderCodegen: func(buf *bytes.Buffer) engineResult {
				d := formFixture()
				render := func() {
					buf.Reset()
					if err := CGForm(buf, d); err != nil {
						panic(err)
					}
				}
				ns, allocs, rps := measure(render)
				buf.Reset()
				_ = CGForm(buf, d)
				return engineResult{html: buf.String(), nsPerOp: ns, allocsPerOp: allocs, rendersPerSec: rps}
			},
			renderJoker: func(buf *bytes.Buffer) engineResult {
				d := formFixture()
				render := func() {
					buf.Reset()
					Form(d, buf)
				}
				ns, allocs, rps := measure(render)
				buf.Reset()
				Form(d, buf)
				return engineResult{html: buf.String(), nsPerOp: ns, allocsPerOp: allocs, rendersPerSec: rps}
			},
		},
		{
			name:       "blog",
			pugFile:    "blog.pug",
			interpData: blogMap(),
			renderCodegen: func(buf *bytes.Buffer) engineResult {
				d := blogFixture()
				render := func() {
					buf.Reset()
					if err := CGBlog(buf, d); err != nil {
						panic(err)
					}
				}
				ns, allocs, rps := measure(render)
				buf.Reset()
				_ = CGBlog(buf, d)
				return engineResult{html: buf.String(), nsPerOp: ns, allocsPerOp: allocs, rendersPerSec: rps}
			},
			renderJoker: func(buf *bytes.Buffer) engineResult {
				d := blogFixture()
				render := func() {
					buf.Reset()
					Blog(d, buf)
				}
				ns, allocs, rps := measure(render)
				buf.Reset()
				Blog(d, buf)
				return engineResult{html: buf.String(), nsPerOp: ns, allocsPerOp: allocs, rendersPerSec: rps}
			},
		},
	}
}

func templatesDir() (string, error) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("runtime.Caller: could not determine this file's own path")
	}
	moduleDir := filepath.Dir(thisFile)
	return filepath.Join(filepath.Dir(moduleDir), "templates"), nil
}

func runInterp(dir string, tc templateCase) engineResult {
	src, err := os.ReadFile(filepath.Join(dir, tc.pugFile))
	if err != nil {
		panic(err)
	}
	tpl, err := gopug.Compile(string(src), &gopug.Options{Basedir: dir})
	if err != nil {
		panic(fmt.Errorf("compiling %s: %w", tc.pugFile, err))
	}
	html, err := tpl.Render(tc.interpData)
	if err != nil {
		panic(err)
	}
	sinkLen := 0
	render := func() {
		out, rerr := tpl.Render(tc.interpData)
		if rerr != nil {
			panic(rerr)
		}
		sinkLen += len(out)
	}
	ns, allocs, rps := measure(render)
	_ = sinkLen
	return engineResult{html: html, nsPerOp: ns, allocsPerOp: allocs, rendersPerSec: rps}
}

func main() {
	dir, err := templatesDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Printf("=== go-pug vs Joker/jade v1.1.3: %d templates ===\n", len(cases()))
	fmt.Printf("%s %s GOMAXPROCS=%d\n\n", runtime.Version(), runtime.GOOS+"/"+runtime.GOARCH, runtime.GOMAXPROCS(0))

	type row struct {
		name             string
		interp, cg, jk   engineResult
		identical        bool
		formIdentical    bool
		knownVoidDiverge bool
	}
	var rows []row

	var buf bytes.Buffer
	for _, tc := range cases() {
		fmt.Printf("interpreter  %-10s ... ", tc.name)
		interp := runInterp(dir, tc)
		fmt.Printf("%12.0f renders/sec  %6.0f allocs/op\n", interp.rendersPerSec, interp.allocsPerOp)

		fmt.Printf("codegen      %-10s ... ", tc.name)
		cg := tc.renderCodegen(&buf)
		fmt.Printf("%12.0f renders/sec  %6.0f allocs/op\n", cg.rendersPerSec, cg.allocsPerOp)

		fmt.Printf("joker        %-10s ... ", tc.name)
		jk := tc.renderJoker(&buf)
		fmt.Printf("%12.0f renders/sec  %6.0f allocs/op\n\n", jk.rendersPerSec, jk.allocsPerOp)

		a := trimTrailingNewline(interp.html)
		b := trimTrailingNewline(cg.html)
		c := trimTrailingNewline(jk.html)
		identical := a == b && a == c
		formIdentical := a == b && trimTrailingNewline(stripVoidSelfClose(jk.html)) == a
		rows = append(rows, row{
			name: tc.name, interp: interp, cg: cg, jk: jk,
			identical: identical, formIdentical: formIdentical,
			knownVoidDiverge: !identical && formIdentical,
		})
	}

	fmt.Println("=== renders/sec (higher is better) ===")
	fmt.Printf("%-12s %14s %14s %14s\n", "template", "interpreter", "codegen", "joker")
	for _, r := range rows {
		fmt.Printf("%-12s %14.0f %14.0f %14.0f\n", r.name, r.interp.rendersPerSec, r.cg.rendersPerSec, r.jk.rendersPerSec)
	}

	fmt.Println("\n=== allocs/op ===")
	fmt.Printf("%-12s %14s %14s %14s\n", "template", "interpreter", "codegen", "joker")
	for _, r := range rows {
		fmt.Printf("%-12s %14.0f %14.0f %14.0f\n", r.name, r.interp.allocsPerOp, r.cg.allocsPerOp, r.jk.allocsPerOp)
	}

	fmt.Println("\n=== ns/op ===")
	fmt.Printf("%-12s %14s %14s %14s\n", "template", "interpreter", "codegen", "joker")
	for _, r := range rows {
		fmt.Printf("%-12s %14.1f %14.1f %14.1f\n", r.name, r.interp.nsPerOp, r.cg.nsPerOp, r.jk.nsPerOp)
	}

	fmt.Println("\n=== output fidelity (byte-identical after trimming one trailing newline) ===")
	allGood := true
	for _, r := range rows {
		switch {
		case r.identical:
			fmt.Printf("%-12s byte-identical across all three engines\n", r.name)
		case r.knownVoidDiverge:
			fmt.Printf("%-12s interpreter == codegen; joker differs only by self-closing void elements (known, see README)\n", r.name)
		default:
			fmt.Printf("%-12s MISMATCH (unexpected) — interpreter=%d codegen=%d joker=%d bytes\n", r.name, len(r.interp.html), len(r.cg.html), len(r.jk.html))
			allGood = false
		}
	}
	if !allGood {
		fmt.Fprintln(os.Stderr, "\nunexpected output mismatch — see above")
		os.Exit(1)
	}

	fmt.Println("\ndone. See README.md for methodology, the disclosed Joker-favorable writer tweak, and fairness caveats.")
}
