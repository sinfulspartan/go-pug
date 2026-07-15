// Command benchmark measures and charts go-pug's render throughput against
// pug.js 3.0.4, three ways: pug.js itself (Node), the go-pug interpreter
// (Template.Render), and go-pug's typed codegen (a generated Go render
// function, built once and called directly). See benchmark/README.md for
// the full methodology and how to reproduce a run.
//
// Usage: go run ./benchmark
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sinfulspartan/go-pug/benchmark/chartlib"
	"github.com/sinfulspartan/go-pug/pkg/gopug"
)

// The timing scheme below is applied identically, in all three engines, to
// keep the comparison apples-to-apples: a short calibration run estimates
// renders/second, which sizes a per-repetition iteration count N aimed at
// targetRepSeconds of wall time (clamped to [minIterations,maxIterations]);
// each of repetitions repetitions discards a warmupFraction-sized warmup
// before its own timed N iterations; the reported figure is the median
// renders/second across the repetitions. The Node leg (bench_pugjs.mjs)
// implements the exact same constants and algorithm independently, since JS
// and Go cannot share code directly.
const (
	calibrationIterations = 30
	targetRepSeconds      = 0.4
	minIterations         = 200
	maxIterations         = 3_000_000
	warmupFraction        = 0.15
	repetitions           = 5
)

func median5(v [repetitions]float64) float64 {
	s := v[:]
	sort.Float64s(s)
	return s[len(s)/2]
}

// measureRendersPerSec times only render — no compilation, no parsing, no
// file I/O — using the calibrate/warm-up/repeat/median scheme documented
// above.
func measureRendersPerSec(render func()) float64 {
	for i := 0; i < calibrationIterations; i++ {
		render()
	}
	start := time.Now()
	for i := 0; i < calibrationIterations; i++ {
		render()
	}
	rate := float64(calibrationIterations) / elapsedSeconds(start)

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
	for r := 0; r < repetitions; r++ {
		for i := 0; i < warmup; i++ {
			render()
		}
		start := time.Now()
		for i := 0; i < n; i++ {
			render()
		}
		repRates[r] = float64(n) / elapsedSeconds(start)
	}
	return median5(repRates)
}

func elapsedSeconds(start time.Time) float64 {
	el := time.Since(start).Seconds()
	if el <= 0 {
		return 1e-9
	}
	return el
}

func trimTrailingNewline(s string) string {
	return strings.TrimSuffix(s, "\n")
}

// repoLayout locates the repository root, benchmark directory, and
// templates directory from this file's own path, so the program works
// regardless of the caller's current working directory.
func repoLayout() (repoRoot, benchDir, templatesDir string, err error) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", "", "", fmt.Errorf("runtime.Caller: could not determine this file's own path")
	}
	benchDir = filepath.Dir(thisFile)
	repoRoot = filepath.Dir(benchDir)
	templatesDir = filepath.Join(benchDir, "templates")
	return repoRoot, benchDir, templatesDir, nil
}

// repoModuleReplaceDirectives are the extra go.mod lines a throwaway module
// needs to resolve github.com/sinfulspartan/go-pug against this checkout
// instead of a module proxy, ported from
// perf-compare/codegen-e2e/main.go's identically named helper.
func repoModuleReplaceDirectives(repoRoot string) string {
	return fmt.Sprintf("\nrequire github.com/sinfulspartan/go-pug v0.0.0\n\nreplace github.com/sinfulspartan/go-pug => %s\n", strconv.Quote(repoRoot))
}

// buildAndRunCodegenBench scaffolds a throwaway module around a GenerateGo
// result, splices in structSrc and a bench-mode main() that renders once
// (into a buffer, to capture the HTML for the byte-identity check) and then
// runs the same calibrate/warm-up/repeat/median timing loop
// measureRendersPerSec implements in this package, timing only calls to the
// generated Render function against io.Discard, builds the module, runs it,
// and decodes its {html, rendersPerSec} JSON stdout.
func buildAndRunCodegenBench(repoRoot string, generated []byte, structSrc, dataLiteralSrc string) (html string, rendersPerSec float64, err error) {
	dir, err := os.MkdirTemp("", "gopug-bench-*")
	if err != nil {
		return "", 0, fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	goMod := "module gopugbench\n\ngo 1.26\n" + repoModuleReplaceDirectives(repoRoot)
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(goMod), 0o644); err != nil {
		return "", 0, fmt.Errorf("writing go.mod: %w", err)
	}

	genStr := string(generated)
	funcIdx := strings.Index(genStr, "\nfunc ")
	if funcIdx < 0 {
		return "", 0, fmt.Errorf("generated source has no \"func \" marker to splice the struct declaration before:\n%s", genStr)
	}

	// The generated file always imports "io" itself (Render's signature
	// takes an io.Writer), so this appended import block deliberately omits
	// it — importing the same package path twice in one file is a compile
	// error. It also avoids "sort" (the bench main below sorts its 5
	// repetition rates with a tiny inline insertion sort instead) since
	// whether the generated file itself needs "sort" depends on the
	// template and isn't known here.
	var src strings.Builder
	src.WriteString(genStr[:funcIdx])
	src.WriteString("\n\nimport (\n\t\"bytes\"\n\t\"encoding/json\"\n\t\"os\"\n\t\"time\"\n)\n\n")
	src.WriteString(structSrc)
	src.WriteString(genStr[funcIdx:])
	src.WriteString(codegenBenchMainSrc(dataLiteralSrc))

	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src.String()), 0o644); err != nil {
		return "", 0, fmt.Errorf("writing main.go: %w", err)
	}

	exeName := "app"
	if runtime.GOOS == "windows" {
		exeName += ".exe"
	}
	exePath := filepath.Join(dir, exeName)

	buildCmd := exec.Command("go", "build", "-o", exePath, ".")
	buildCmd.Dir = dir
	if out, err := buildCmd.CombinedOutput(); err != nil {
		return "", 0, fmt.Errorf("go build: %w\n%s\n--- source ---\n%s", err, out, src.String())
	}

	var outBuf, errBuf bytes.Buffer
	runCmd := exec.Command(exePath)
	runCmd.Stdout = &outBuf
	runCmd.Stderr = &errBuf
	if err := runCmd.Run(); err != nil {
		return "", 0, fmt.Errorf("running generated bench binary: %w\n%s", err, errBuf.String())
	}

	var decoded struct {
		HTML          string  `json:"html"`
		RendersPerSec float64 `json:"rendersPerSec"`
	}
	if err := json.Unmarshal(outBuf.Bytes(), &decoded); err != nil {
		return "", 0, fmt.Errorf("decoding bench binary output %q: %w", outBuf.String(), err)
	}
	return decoded.HTML, decoded.RendersPerSec, nil
}

// codegenBenchMainSrc is the bench-mode main() spliced after the generated
// Render function: it renders once to capture the HTML, then applies the
// exact same calibrate/warm-up/repeat/median-of-5 scheme
// measureRendersPerSec (this package) and bench_pugjs.mjs's
// measureRendersPerSec (Node) apply, timing only Render(io.Discard, d)
// calls, and prints {html, rendersPerSec} as JSON.
func codegenBenchMainSrc(dataLiteralSrc string) string {
	return fmt.Sprintf(`
func medianOf5(v [5]float64) float64 {
	for i := 0; i < len(v); i++ {
		for j := i + 1; j < len(v); j++ {
			if v[j] < v[i] {
				v[i], v[j] = v[j], v[i]
			}
		}
	}
	return v[2]
}

func main() {
	d := %s

	var buf bytes.Buffer
	if err := Render(&buf, d); err != nil {
		panic(err)
	}
	html := buf.String()

	const calibIters = 30
	for i := 0; i < calibIters; i++ {
		if err := Render(io.Discard, d); err != nil {
			panic(err)
		}
	}
	start := time.Now()
	for i := 0; i < calibIters; i++ {
		if err := Render(io.Discard, d); err != nil {
			panic(err)
		}
	}
	elapsed := time.Since(start).Seconds()
	if elapsed <= 0 {
		elapsed = 1e-9
	}
	rate := float64(calibIters) / elapsed

	n := int(rate * 0.4)
	if n < 200 {
		n = 200
	}
	if n > 3000000 {
		n = 3000000
	}
	warmup := int(float64(n) * 0.15)
	if warmup < 1 {
		warmup = 1
	}

	var repRates [5]float64
	for r := 0; r < 5; r++ {
		for i := 0; i < warmup; i++ {
			if err := Render(io.Discard, d); err != nil {
				panic(err)
			}
		}
		st := time.Now()
		for i := 0; i < n; i++ {
			if err := Render(io.Discard, d); err != nil {
				panic(err)
			}
		}
		el := time.Since(st).Seconds()
		if el <= 0 {
			el = 1e-9
		}
		repRates[r] = float64(n) / el
	}
	median := medianOf5(repRates)

	out := struct {
		HTML          string  `+"`json:\"html\"`"+`
		RendersPerSec float64 `+"`json:\"rendersPerSec\"`"+`
	}{HTML: html, RendersPerSec: median}
	enc := json.NewEncoder(os.Stdout)
	if err := enc.Encode(out); err != nil {
		panic(err)
	}
}
`, dataLiteralSrc)
}

// runInterpLeg compiles the template once, renders it once (for the
// byte-identity check), then times only Template.Render calls.
func runInterpLeg(templatesDir string, tc benchCase) (html string, rendersPerSec float64, err error) {
	path := filepath.Join(templatesDir, tc.template)
	src, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	tpl, err := gopug.Compile(string(src), &gopug.Options{Basedir: templatesDir})
	if err != nil {
		return "", 0, fmt.Errorf("compile: %w", err)
	}

	html, err = tpl.Render(tc.interpData)
	if err != nil {
		return "", 0, fmt.Errorf("render: %w", err)
	}

	sinkLen := 0
	rendersPerSec = measureRendersPerSec(func() {
		out, rerr := tpl.Render(tc.interpData)
		if rerr != nil {
			panic(rerr)
		}
		sinkLen += len(out)
	})
	_ = sinkLen

	return html, rendersPerSec, nil
}

type engineOutcome struct {
	html          string
	rendersPerSec float64
}

func detectGoVersion() string {
	return runtime.Version()
}

func detectNodeVersion() string {
	out, err := exec.Command("node", "--version").Output()
	if err != nil {
		return "unknown (node --version failed: " + err.Error() + ")"
	}
	return strings.TrimSpace(string(out))
}

func detectPugVersion(benchDir string) string {
	data, err := os.ReadFile(filepath.Join(benchDir, "node_modules", "pug", "package.json"))
	if err != nil {
		return "unknown (run `npm install` in benchmark/ first)"
	}
	var pkg struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return "unknown (could not parse node_modules/pug/package.json)"
	}
	return pkg.Version
}

func detectCPUModel() string {
	if v := os.Getenv("BENCH_CPU_MODEL"); v != "" {
		return v
	}
	switch runtime.GOOS {
	case "windows":
		out, err := exec.Command("wmic", "cpu", "get", "name").Output()
		if err == nil {
			lines := strings.Split(strings.ReplaceAll(string(out), "\r", ""), "\n")
			for _, l := range lines {
				l = strings.TrimSpace(l)
				if l != "" && l != "Name" {
					return l
				}
			}
		}
	case "linux":
		data, err := os.ReadFile("/proc/cpuinfo")
		if err == nil {
			for _, l := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(l, "model name") {
					if idx := strings.Index(l, ":"); idx >= 0 {
						return strings.TrimSpace(l[idx+1:])
					}
				}
			}
		}
	case "darwin":
		out, err := exec.Command("sysctl", "-n", "machdep.cpu.brand_string").Output()
		if err == nil {
			return strings.TrimSpace(string(out))
		}
	}
	return "unknown (set BENCH_CPU_MODEL to override)"
}

func main() {
	repoRoot, benchDir, templatesDir, err := repoLayout()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	cases := benchCases()

	fmt.Printf("=== go-pug 3-way render benchmark: %d templates ===\n\n", len(cases))

	interpOutcomes := make(map[string]engineOutcome, len(cases))
	codegenOutcomes := make(map[string]engineOutcome, len(cases))

	for _, tc := range cases {
		fmt.Printf("interpreter  %-20s ... ", tc.name)
		html, rps, err := runInterpLeg(templatesDir, tc)
		if err != nil {
			fmt.Println("FAIL")
			fmt.Fprintf(os.Stderr, "interpreter leg failed for %s: %v\n", tc.name, err)
			os.Exit(1)
		}
		interpOutcomes[tc.name] = engineOutcome{html: html, rendersPerSec: rps}
		fmt.Printf("%.0f renders/sec\n", rps)

		fmt.Printf("codegen      %-20s ... ", tc.name)
		ast, err := gopug.Parse(mustReadFile(filepath.Join(templatesDir, tc.template)), &gopug.Options{Basedir: templatesDir})
		if err != nil {
			fmt.Println("FAIL")
			fmt.Fprintf(os.Stderr, "codegen leg: parse failed for %s: %v\n", tc.name, err)
			os.Exit(1)
		}
		resolved, err := gopug.ResolveComposition(ast, &gopug.Options{Basedir: templatesDir})
		if err != nil {
			fmt.Println("FAIL")
			fmt.Fprintf(os.Stderr, "codegen leg: ResolveComposition failed for %s: %v\n", tc.name, err)
			os.Exit(1)
		}
		generated, err := gopug.GenerateGo(resolved, gopug.Config{
			PackageName:     "main",
			FuncName:        "Render",
			DataType:        tc.dataType,
			DataReflectType: tc.reflectType,
		})
		if err != nil {
			fmt.Println("FAIL")
			fmt.Fprintf(os.Stderr, "codegen leg: GenerateGo DEFERRED for %s (this template does not belong in the corpus): %v\n", tc.name, err)
			os.Exit(1)
		}
		cgHTML, cgRPS, err := buildAndRunCodegenBench(repoRoot, generated, tc.structSrc, tc.dataLiteralSrc)
		if err != nil {
			fmt.Println("FAIL")
			fmt.Fprintf(os.Stderr, "codegen leg: build/run failed for %s: %v\n", tc.name, err)
			os.Exit(1)
		}
		codegenOutcomes[tc.name] = engineOutcome{html: cgHTML, rendersPerSec: cgRPS}
		fmt.Printf("%.0f renders/sec\n", cgRPS)
	}

	// pug.js leg: one Node process handles every template, so its own
	// module-load and JIT warm-up cost is paid once rather than once per
	// template.
	manifest := make([]map[string]any, 0, len(cases))
	for _, tc := range cases {
		manifest = append(manifest, map[string]any{
			"name":     tc.name,
			"template": tc.template,
			"locals":   tc.interpData,
		})
	}
	tmpDir, err := os.MkdirTemp("", "gopug-bench-node-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.RemoveAll(tmpDir)

	manifestPath := filepath.Join(tmpDir, "manifest.json")
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(manifestPath, manifestJSON, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	outputPath := filepath.Join(tmpDir, "pugjs-output.json")

	fmt.Printf("\npug.js       running %d templates via Node ...\n", len(cases))
	nodeCmd := exec.Command("node", filepath.Join(benchDir, "bench_pugjs.mjs"), manifestPath, templatesDir, outputPath)
	nodeCmd.Dir = benchDir
	if out, err := nodeCmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "pug.js leg failed: %v\n%s\n", err, out)
		os.Exit(1)
	}

	pugOutputBytes, err := os.ReadFile(outputPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	var pugOutput struct {
		Results []struct {
			Name          string  `json:"name"`
			HTML          string  `json:"html"`
			RendersPerSec float64 `json:"rendersPerSec"`
		} `json:"results"`
	}
	if err := json.Unmarshal(pugOutputBytes, &pugOutput); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	pugOutcomes := make(map[string]engineOutcome, len(pugOutput.Results))
	for _, r := range pugOutput.Results {
		pugOutcomes[r.Name] = engineOutcome{html: r.HTML, rendersPerSec: r.RendersPerSec}
		fmt.Printf("pug.js       %-20s ... %.0f renders/sec\n", r.Name, r.RendersPerSec)
	}

	// Assert byte-identity BEFORE trusting any timing number: a template
	// that codegen defers or diverges on must never end up in results.json.
	templateResults := make([]chartlib.TemplateResult, 0, len(cases))
	for _, tc := range cases {
		interp, ok := interpOutcomes[tc.name]
		if !ok {
			fmt.Fprintf(os.Stderr, "internal error: no interpreter outcome for %s\n", tc.name)
			os.Exit(1)
		}
		codegen, ok := codegenOutcomes[tc.name]
		if !ok {
			fmt.Fprintf(os.Stderr, "internal error: no codegen outcome for %s\n", tc.name)
			os.Exit(1)
		}
		pug, ok := pugOutcomes[tc.name]
		if !ok {
			fmt.Fprintf(os.Stderr, "internal error: no pug.js outcome for %s\n", tc.name)
			os.Exit(1)
		}

		a := trimTrailingNewline(pug.html)
		b := trimTrailingNewline(interp.html)
		c := trimTrailingNewline(codegen.html)
		if a != b || a != c {
			fmt.Fprintf(os.Stderr, "\nBYTE-IDENTITY ASSERTION FAILED for %s — refusing to time or record this template\n", tc.name)
			fmt.Fprintf(os.Stderr, "pug.js:\n%q\n\ninterpreter:\n%q\n\ncodegen:\n%q\n", a, b, c)
			os.Exit(1)
		}

		templateResults = append(templateResults, chartlib.TemplateResult{
			Name:                     tc.name,
			Description:              tc.description,
			PugjsRendersPerSec:       pug.rendersPerSec,
			InterpreterRendersPerSec: interp.rendersPerSec,
			CodegenRendersPerSec:     codegen.rendersPerSec,
		})
	}

	results := chartlib.Results{
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Methodology: chartlib.Methodology{
			Description:           "Every engine is pre-compiled/pre-generated exactly once, outside the timed loop: pug.js via pug.compileFile, the interpreter via gopug.Compile, codegen via GenerateGo + `go build`. Only the render call itself is timed, rendering the same fixture data every iteration to a discarded sink (io.Discard for codegen; the interpreter's returned string is discarded after a length checksum; pug.js accumulates a length checksum so V8 cannot treat the calls as dead code).",
			Metric:                "renders per second (higher is better)",
			CalibrationIterations: calibrationIterations,
			TargetRepSeconds:      targetRepSeconds,
			MinIterations:         minIterations,
			MaxIterations:         maxIterations,
			WarmupFraction:        warmupFraction,
			Repetitions:           repetitions,
			Aggregation:           "median of 5 repetitions, each preceded by its own discarded warmup",
			Sink:                  "codegen: io.Discard; interpreter: Template.Render's returned string, discarded after accumulating its length; pug.js: the rendered string's length accumulated into a checksum written to the raw Node-leg output",
			ByteIdentityAssertion: "For every template, all three engines are rendered once (outside the timed loop) and compared byte-for-byte after trimming a single trailing newline; a template is only timed, and only appears in this file, if pug.js, the interpreter, and codegen produced identical output",
		},
		Machine: chartlib.Machine{
			OS:          runtime.GOOS + "/" + runtime.GOARCH,
			CPU:         detectCPUModel(),
			GoVersion:   detectGoVersion(),
			NodeVersion: detectNodeVersion(),
			PugVersion:  detectPugVersion(benchDir),
		},
		Caveats: []string{
			"This is a cross-runtime comparison: pug.js runs on Node/V8 (a JIT-compiled dynamic language) and go-pug codegen runs as compiled, ahead-of-time Go — it answers \"what would I deploy\", not \"which language is faster\" in the abstract.",
			"Absolute renders/second is machine-dependent; only relative ordering and rough magnitude should be expected to reproduce on a different machine.",
			"The corpus is deliberately restricted to templates where go-pug codegen fully supports the template (no fallback/defer) and all three engines produce byte-identical HTML, so every number here measures equivalent rendered output.",
		},
		Templates: templateResults,
	}

	resultsJSON, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(filepath.Join(benchDir, "results.json"), resultsJSON, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("\nwrote %s\n", filepath.Join(benchDir, "results.json"))

	svg := chartlib.GenerateSVG(results)
	if err := os.WriteFile(filepath.Join(benchDir, "chart.svg"), svg, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s\n", filepath.Join(benchDir, "chart.svg"))

	fmt.Println("\nall templates byte-identical across pug.js, interpreter, and codegen; see results.json / chart.svg")
}

func mustReadFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		panic(err)
	}
	return string(data)
}
