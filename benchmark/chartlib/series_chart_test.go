package chartlib

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// TestGenerateSVGUnaffectedByGenericAddition guards against the risk that
// generalizing chartlib for GenerateSVGSeries silently changed
// GenerateSVG's own output: it re-renders the committed benchmark/chart.svg
// from the committed benchmark/results.json and asserts byte-for-byte
// equality with the file already on disk.
func TestGenerateSVGUnaffectedByGenericAddition(t *testing.T) {
	data, err := os.ReadFile("../results.json")
	if err != nil {
		t.Fatalf("reading ../results.json: %v", err)
	}
	var results Results
	if err := json.Unmarshal(data, &results); err != nil {
		t.Fatalf("parsing ../results.json: %v", err)
	}

	got := GenerateSVG(results)

	want, err := os.ReadFile("../chart.svg")
	if err != nil {
		t.Fatalf("reading ../chart.svg: %v", err)
	}

	if string(got) != string(want) {
		t.Fatalf("GenerateSVG output no longer matches the committed benchmark/chart.svg byte-for-byte")
	}
}

func TestGenerateSVGSeriesRendersLabelsColorsAndValues(t *testing.T) {
	categories := []string{"card_list", "table"}
	series := []Series{
		{Label: "go-pug interpreter", Color: "#0072b2", Values: []float64{36427, 35953}},
		{Label: "go-pug codegen", Color: "#009e73", Values: []float64{622523, 358465}},
		{Label: "Joker/jade", Color: "#cc79a7", Values: []float64{131430, 107737}},
	}

	svg := string(GenerateSVGSeries("go-pug vs Joker/jade", "renders/second, log scale", categories, series))

	if !strings.HasPrefix(svg, "<svg ") {
		t.Fatalf("output does not start with an <svg> tag: %q", svg[:min(40, len(svg))])
	}
	if !strings.Contains(svg, "go-pug vs Joker/jade") {
		t.Error("missing chart title")
	}
	if !strings.Contains(svg, "renders/second, log scale") {
		t.Error("missing chart subtitle")
	}
	for _, cat := range categories {
		if !strings.Contains(svg, cat) {
			t.Errorf("missing category label %q", cat)
		}
	}
	for _, s := range series {
		if !strings.Contains(svg, s.Label) {
			t.Errorf("missing series label %q", s.Label)
		}
		if !strings.Contains(svg, s.Color) {
			t.Errorf("missing series color %q", s.Color)
		}
	}
	// One of the largest values should appear as a formatted, comma-grouped
	// bar-value label, same as GenerateSVG's per-bar labels.
	if !strings.Contains(svg, "622,523") {
		t.Error("missing formatted value label for 622523")
	}
	if !strings.Contains(svg, "36,427") {
		t.Error("missing formatted value label for 36427")
	}
}

func TestGenerateSVGSeriesHandlesEmptyInputsWithoutPanicking(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("GenerateSVGSeries panicked on empty input: %v", r)
		}
	}()
	GenerateSVGSeries("empty", "empty", nil, nil)
}
