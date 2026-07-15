// Command chartgen regenerates benchmark/chart.svg from benchmark/results.json
// without re-running the benchmark itself — a standalone, re-runnable step
// for anyone who wants to tweak the chart's rendering without re-measuring.
//
// Usage: go run ./benchmark/chartgen [resultsPath] [chartPath]
// Both arguments default to results.json/chart.svg next to this program's
// own directory (i.e. benchmark/).
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/sinfulspartan/go-pug/benchmark/chartlib"
)

func main() {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		fmt.Fprintln(os.Stderr, "chartgen: could not determine this program's own path")
		os.Exit(1)
	}
	benchDir := filepath.Dir(filepath.Dir(thisFile))

	resultsPath := filepath.Join(benchDir, "results.json")
	chartPath := filepath.Join(benchDir, "chart.svg")
	if len(os.Args) > 1 {
		resultsPath = os.Args[1]
	}
	if len(os.Args) > 2 {
		chartPath = os.Args[2]
	}

	data, err := os.ReadFile(resultsPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "chartgen: reading %s: %v\n", resultsPath, err)
		os.Exit(1)
	}
	var results chartlib.Results
	if err := json.Unmarshal(data, &results); err != nil {
		fmt.Fprintf(os.Stderr, "chartgen: parsing %s: %v\n", resultsPath, err)
		os.Exit(1)
	}
	if len(results.Templates) == 0 {
		fmt.Fprintf(os.Stderr, "chartgen: %s has no templates\n", resultsPath)
		os.Exit(1)
	}

	svg := chartlib.GenerateSVG(results)
	if err := os.WriteFile(chartPath, svg, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "chartgen: writing %s: %v\n", chartPath, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%d templates)\n", chartPath, len(results.Templates))
}
