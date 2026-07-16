// Command chartgen renders benchmark/vs-joker/chart.svg from
// benchmark/vs-joker/results.json, the single source of truth for the
// numbers also shown in README.md's comparison table — so the chart and the
// table can never drift apart.
//
// Usage, from benchmark/vs-joker:
//
//	go run ./chartgen [resultsPath] [chartPath]
//
// Both arguments default to results.json/chart.svg next to this program's
// own parent directory (i.e. benchmark/vs-joker/).
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
	vsJokerDir := filepath.Dir(filepath.Dir(thisFile))

	resultsPath := filepath.Join(vsJokerDir, "results.json")
	chartPath := filepath.Join(vsJokerDir, "chart.svg")
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
	var doc chartlib.SeriesChartData
	if err := json.Unmarshal(data, &doc); err != nil {
		fmt.Fprintf(os.Stderr, "chartgen: parsing %s: %v\n", resultsPath, err)
		os.Exit(1)
	}
	if len(doc.Categories) == 0 || len(doc.Series) == 0 {
		fmt.Fprintf(os.Stderr, "chartgen: %s has no categories/series\n", resultsPath)
		os.Exit(1)
	}

	svg := chartlib.GenerateSVGSeries(doc.Title, doc.Subtitle, doc.Categories, doc.Series)
	if err := os.WriteFile(chartPath, svg, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "chartgen: writing %s: %v\n", chartPath, err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%d categories, %d series)\n", chartPath, len(doc.Categories), len(doc.Series))
}
