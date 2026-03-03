// bench2md converts the output of `go test -bench` (read from stdin) into a
// human-readable report written to stdout or a file.
//
// Usage:
//
//	go test -bench . -benchmem ./pkg/gopug | go run ./scripts/bench2md
//
// Optional flags:
//
//	-o <file>        write output to a file instead of stdout
//	-format <fmt>    output format: md (default), json, csv
package main

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Data model
// ---------------------------------------------------------------------------

// result holds the parsed data for a single benchmark line.
type result struct {
	name    string  // stripped name, e.g. "CompileSmall"
	group   string  // section heading
	iters   int64
	ns      float64 // ns/op
	bop     float64 // B/op  (-1 = not present)
	aop     int64   // allocs/op  (-1 = not present)
	hasMem  bool
}

// group is an ordered collection of results under a section heading.
type group struct {
	heading string
	rows    []result
}

// report holds everything parsed from the benchmark output.
type report struct {
	goos    string
	goarch  string
	cpu     string
	pkg     string
	elapsed string
	groups  []group // in order of first appearance
}

// allResults returns every result across all groups in order.
func (rep *report) allResults() []result {
	var out []result
	for _, g := range rep.groups {
		out = append(out, g.rows...)
	}
	return out
}

// ---------------------------------------------------------------------------
// Grouping
// ---------------------------------------------------------------------------

// groupOrder controls the order sections appear in the output.
// Any name not matched lands in "Other" at the end.
var groupOrder = []string{
	"Compile",
	"CompileFile",
	"Render",
	"RenderFile",
	"End-to-End (compile + render)",
	"Other",
}

func groupFor(name string) string {
	switch {
	case strings.HasPrefix(name, "CompileFile"):
		return "CompileFile"
	case strings.HasPrefix(name, "Compile"):
		return "Compile"
	case strings.HasPrefix(name, "RenderFile"):
		return "RenderFile"
	case strings.HasPrefix(name, "Render"):
		return "Render"
	case strings.HasPrefix(name, "E2E"):
		return "End-to-End (compile + render)"
	default:
		return "Other"
	}
}

// ---------------------------------------------------------------------------
// Parsing
// ---------------------------------------------------------------------------

// benchmarkLine matches a standard benchmark output line.
// Example:
//
//	BenchmarkCompileSmall-16    266112    1700 ns/op    1448 B/op    47 allocs/op
var benchLine = regexp.MustCompile(
	`^(Benchmark\S+)\s+(\d+)\s+([\d.]+)\s+ns/op` +
		`(?:\s+([\d.]+)\s+B/op\s+(\d+)\s+allocs/op)?`,
)

// elapsedLine matches the final "ok  pkg  1.234s" line.
var elapsedLine = regexp.MustCompile(`\b(\d+\.\d+s)\s*$`)

// stripSuffix removes the leading "Benchmark" prefix and trailing "-N"
// GOMAXPROCS suffix from a raw name like "BenchmarkCompileSmall-16".
func stripSuffix(raw string) string {
	s := strings.TrimPrefix(raw, "Benchmark")
	if idx := strings.LastIndex(s, "-"); idx >= 0 {
		tail := s[idx+1:]
		if _, err := strconv.Atoi(tail); err == nil {
			s = s[:idx]
		}
	}
	return s
}

func parse(r io.Reader) report {
	var rep report
	groupMap := make(map[string]*group) // heading -> *group

	// Pre-populate groups in the desired order so output order is stable.
	for _, h := range groupOrder {
		g := &group{heading: h}
		groupMap[h] = g
		rep.groups = append(rep.groups, *g) // placeholder; we update via map
	}

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()

		switch {
		case strings.HasPrefix(line, "goos:"):
			rep.goos = strings.TrimSpace(strings.TrimPrefix(line, "goos:"))

		case strings.HasPrefix(line, "goarch:"):
			rep.goarch = strings.TrimSpace(strings.TrimPrefix(line, "goarch:"))

		case strings.HasPrefix(line, "cpu:"):
			rep.cpu = strings.TrimSpace(strings.TrimPrefix(line, "cpu:"))

		case strings.HasPrefix(line, "pkg:"):
			rep.pkg = strings.TrimSpace(strings.TrimPrefix(line, "pkg:"))

		case strings.HasPrefix(line, "ok "):
			if m := elapsedLine.FindStringSubmatch(line); m != nil {
				rep.elapsed = m[1]
			}

		case strings.HasPrefix(line, "Benchmark"):
			m := benchLine.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			name := stripSuffix(m[1])
			iters, _ := strconv.ParseInt(m[2], 10, 64)
			ns, _ := strconv.ParseFloat(m[3], 64)
			bop := -1.0
			aop := int64(-1)
			hasMem := false
			if m[4] != "" {
				bop, _ = strconv.ParseFloat(m[4], 64)
				aop, _ = strconv.ParseInt(m[5], 10, 64)
				hasMem = true
			}
			h := groupFor(name)
			res := result{
				name:   name,
				group:  h,
				iters:  iters,
				ns:     ns,
				bop:    bop,
				aop:    aop,
				hasMem: hasMem,
			}
			groupMap[h].rows = append(groupMap[h].rows, res)
		}
	}

	// Write the updated group data back into rep.groups in order.
	for i, h := range groupOrder {
		rep.groups[i] = *groupMap[h]
	}
	return rep
}

// ---------------------------------------------------------------------------
// Formatting helpers (shared by md and csv)
// ---------------------------------------------------------------------------

// comma formats an integer with thousands separators: 1234567 -> "1,234,567".
func comma(n int64) string {
	if n < 0 {
		return "-"
	}
	s := strconv.FormatInt(n, 10)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	rem := len(s) % 3
	if rem > 0 {
		b.WriteString(s[:rem])
	}
	for i := rem; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

// humanNS converts a nanosecond float to a readable string.
func humanNS(ns float64) string {
	switch {
	case ns <= 0:
		return "-"
	case ns < 1_000:
		return fmt.Sprintf("%.0f ns", ns)
	case ns < 10_000:
		return fmt.Sprintf("%.2f us", ns/1_000)
	case ns < 1_000_000:
		return fmt.Sprintf("%.0f us", ns/1_000)
	default:
		return fmt.Sprintf("%.2f ms", ns/1_000_000)
	}
}

// humanBytes converts a byte count to a readable string.
func humanBytes(b float64) string {
	switch {
	case b < 0:
		return "-"
	case b == 0:
		return "0 B"
	case b < 1024:
		return fmt.Sprintf("%s B", comma(int64(math.Round(b))))
	case b < 1024*1024:
		return fmt.Sprintf("%.1f KB", b/1024)
	default:
		return fmt.Sprintf("%.1f MB", b/(1024*1024))
	}
}

// ---------------------------------------------------------------------------
// Markdown output
// ---------------------------------------------------------------------------

func renderMarkdown(w io.Writer, rep report, goVersion string) {
	date := time.Now().Format("2006-01-02")

	// Header metadata table
	fmt.Fprintln(w, "# Go-Pug Benchmark Results")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "| | |")
	fmt.Fprintln(w, "|---|---|")
	fmt.Fprintf(w, "| **Date** | %s |\n", date)
	fmt.Fprintf(w, "| **Go** | %s |\n", goVersion)
	if rep.cpu != "" {
		fmt.Fprintf(w, "| **CPU** | %s |\n", rep.cpu)
	}
	if rep.goos != "" {
		fmt.Fprintf(w, "| **OS** | %s / %s |\n", rep.goos, rep.goarch)
	}
	if rep.pkg != "" {
		fmt.Fprintf(w, "| **Package** | `%s` |\n", rep.pkg)
	}
	if rep.elapsed != "" {
		fmt.Fprintf(w, "| **Total time** | %s |\n", rep.elapsed)
	}
	fmt.Fprintln(w)

	// One section per group
	total := 0
	for _, g := range rep.groups {
		if len(g.rows) == 0 {
			continue
		}
		total += len(g.rows)

		fmt.Fprintf(w, "## %s\n\n", g.heading)

		// Determine whether any row has memory stats.
		hasMem := false
		for _, r := range g.rows {
			if r.hasMem {
				hasMem = true
				break
			}
		}

		if hasMem {
			fmt.Fprintln(w, "| Benchmark | Iterations | Time/op | B/op | allocs/op |")
			fmt.Fprintln(w, "|---|---:|---:|---:|---:|")
		} else {
			fmt.Fprintln(w, "| Benchmark | Iterations | Time/op |")
			fmt.Fprintln(w, "|---|---:|---:|")
		}

		for _, r := range g.rows {
			if hasMem {
				fmt.Fprintf(w, "| `%s` | %s | %s | %s | %s |\n",
					r.name,
					comma(r.iters),
					humanNS(r.ns),
					humanBytes(r.bop),
					comma(r.aop),
				)
			} else {
				fmt.Fprintf(w, "| `%s` | %s | %s |\n",
					r.name,
					comma(r.iters),
					humanNS(r.ns),
				)
			}
		}
		fmt.Fprintln(w)
	}

	// Footer
	if total == 0 {
		fmt.Fprintln(w, "> No benchmark results found in input.")
	} else {
		fmt.Fprintln(w, "---")
		fmt.Fprintln(w)
		fmt.Fprintf(w, "_%d benchmarks -- generated by `scripts/bench2md`_\n", total)
	}
	fmt.Fprintln(w)
}

// ---------------------------------------------------------------------------
// CSV output
// ---------------------------------------------------------------------------

func renderCSV(w io.Writer, rep report, goVersion string) {
	cw := csv.NewWriter(w)
	defer cw.Flush()

	// Header row
	cw.Write([]string{ //nolint:errcheck
		"benchmark", "group", "iterations",
		"ns_per_op", "bytes_per_op", "allocs_per_op",
		"go_version", "goos", "goarch", "cpu", "package", "date",
	})

	date := time.Now().Format("2006-01-02")

	for _, r := range rep.allResults() {
		bop := ""
		if r.hasMem {
			bop = strconv.FormatFloat(r.bop, 'f', 2, 64)
		}
		aop := ""
		if r.hasMem {
			aop = strconv.FormatInt(r.aop, 10)
		}
		cw.Write([]string{ //nolint:errcheck
			r.name,
			r.group,
			strconv.FormatInt(r.iters, 10),
			strconv.FormatFloat(r.ns, 'f', 2, 64),
			bop,
			aop,
			goVersion,
			rep.goos,
			rep.goarch,
			rep.cpu,
			rep.pkg,
			date,
		})
	}
}

// ---------------------------------------------------------------------------
// JSON output
// ---------------------------------------------------------------------------

// jsonReport is the top-level JSON document structure.
type jsonReport struct {
	Date       string        `json:"date"`
	GoVersion  string        `json:"go_version"`
	GOOS       string        `json:"goos,omitempty"`
	GOARCH     string        `json:"goarch,omitempty"`
	CPU        string        `json:"cpu,omitempty"`
	Package    string        `json:"package,omitempty"`
	Elapsed    string        `json:"elapsed,omitempty"`
	Benchmarks []jsonResult  `json:"benchmarks"`
}

// jsonResult represents one benchmark result entry.
type jsonResult struct {
	Name        string  `json:"name"`
	Group       string  `json:"group"`
	Iterations  int64   `json:"iterations"`
	NsPerOp     float64 `json:"ns_per_op"`
	BytesPerOp  float64 `json:"bytes_per_op,omitempty"`
	AllocsPerOp int64   `json:"allocs_per_op,omitempty"`
	HasMem      bool    `json:"has_mem"`
}

func renderJSON(w io.Writer, rep report, goVersion string) error {
	doc := jsonReport{
		Date:      time.Now().Format("2006-01-02"),
		GoVersion: goVersion,
		GOOS:      rep.goos,
		GOARCH:    rep.goarch,
		CPU:       rep.cpu,
		Package:   rep.pkg,
		Elapsed:   rep.elapsed,
	}

	for _, r := range rep.allResults() {
		jr := jsonResult{
			Name:       r.name,
			Group:      r.group,
			Iterations: r.iters,
			NsPerOp:    r.ns,
			HasMem:     r.hasMem,
		}
		if r.hasMem {
			jr.BytesPerOp = r.bop
			jr.AllocsPerOp = r.aop
		}
		doc.Benchmarks = append(doc.Benchmarks, jr)
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(doc)
}

// ---------------------------------------------------------------------------
// Entry point
// ---------------------------------------------------------------------------

func main() {
	outFile := flag.String("o", "", "write output to `file` instead of stdout")
	format := flag.String("format", "md", "output format: md, json, csv")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "Usage: go test -bench . -benchmem ./pkg/gopug | go run ./scripts/bench2md [-o file] [-format md|json|csv]")
		flag.PrintDefaults()
	}
	flag.Parse()

	switch *format {
	case "md", "json", "csv":
		// valid
	default:
		fmt.Fprintf(os.Stderr, "bench2md: unknown format %q (choose md, json, or csv)\n", *format)
		os.Exit(1)
	}

	rep := parse(os.Stdin)

	goVersion := fmt.Sprintf("%s %s/%s", runtime.Version(), runtime.GOOS, runtime.GOARCH)

	var out io.Writer = os.Stdout
	if *outFile != "" {
		f, err := os.Create(*outFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "bench2md: cannot create %q: %v\n", *outFile, err)
			os.Exit(1)
		}
		defer f.Close()
		out = f
	}

	switch *format {
	case "md":
		renderMarkdown(out, rep, goVersion)
	case "csv":
		renderCSV(out, rep, goVersion)
	case "json":
		if err := renderJSON(out, rep, goVersion); err != nil {
			fmt.Fprintf(os.Stderr, "bench2md: JSON encode error: %v\n", err)
			os.Exit(1)
		}
	}
}
