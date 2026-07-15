// Package chartlib defines the results.json schema shared by the benchmark
// runner (which produces it) and the standalone chart regenerator (which
// only ever reads it), and renders that schema into a self-contained SVG bar
// chart.
package chartlib

// Methodology records the timing scheme applied identically to all three
// engines, so a reader of results.json can audit exactly how the numbers
// were produced without cross-referencing source code.
type Methodology struct {
	Description           string  `json:"description"`
	Metric                string  `json:"metric"`
	CalibrationIterations int     `json:"calibrationIterations"`
	TargetRepSeconds      float64 `json:"targetRepSeconds"`
	MinIterations         int     `json:"minIterations"`
	MaxIterations         int     `json:"maxIterations"`
	WarmupFraction        float64 `json:"warmupFraction"`
	Repetitions           int     `json:"repetitions"`
	Aggregation           string  `json:"aggregation"`
	Sink                  string  `json:"sink"`
	ByteIdentityAssertion string  `json:"byteIdentityAssertion"`
}

// Machine records the environment the numbers were measured on, since
// render throughput is machine-dependent and this is a cross-runtime
// comparison (Node/V8 vs compiled Go).
type Machine struct {
	OS          string `json:"os"`
	CPU         string `json:"cpu"`
	GoVersion   string `json:"goVersion"`
	NodeVersion string `json:"nodeVersion"`
	PugVersion  string `json:"pugVersion"`
}

// TemplateResult is one template's measured renders/sec for all three
// engines, plus the description shown in the chart legend/README table.
type TemplateResult struct {
	Name                     string  `json:"name"`
	Description              string  `json:"description"`
	PugjsRendersPerSec       float64 `json:"pugjsRendersPerSec"`
	InterpreterRendersPerSec float64 `json:"interpreterRendersPerSec"`
	CodegenRendersPerSec     float64 `json:"codegenRendersPerSec"`
}

// Results is the top-level results.json document.
type Results struct {
	GeneratedAt string           `json:"generatedAt"`
	Methodology Methodology      `json:"methodology"`
	Machine     Machine          `json:"machine"`
	Caveats     []string         `json:"caveats"`
	Templates   []TemplateResult `json:"templates"`
}
