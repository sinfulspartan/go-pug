package chartlib

import (
	"fmt"
	"math"
	"strings"
)

// Series is one named, colored bar series across a shared set of categories
// (e.g. one engine's renders/second across several templates), used by
// GenerateSVGSeries to render an arbitrary-cardinality grouped bar chart.
// Unlike GenerateSVG (fixed at exactly three engines), this supports any
// number of series and categories so callers outside the pug.js/interpreter/
// codegen three-way corpus can reuse the same chart look.
type Series struct {
	Label  string    `json:"label"`
	Color  string    `json:"color"`
	Values []float64 `json:"values"`
}

// SeriesChartData is the JSON schema consumed by GenerateSVGSeries callers:
// a title, subtitle, the shared category labels, and one Series per bar
// color/group.
type SeriesChartData struct {
	Title      string   `json:"title"`
	Subtitle   string   `json:"subtitle"`
	Categories []string `json:"categories"`
	Series     []Series `json:"series"`
}

// estimateTextWidth approximates the rendered pixel width of s at the
// legend's 12.5px font size, for centering the legend row. It is an
// approximation (no text-measurement API in stdlib SVG generation) rather
// than the exact per-label widths GenerateSVG hardcodes for its fixed three
// labels; a few pixels of legend-centering slop is immaterial to legibility.
func estimateTextWidth(s string) float64 {
	return float64(len(s))*7.2 + 2
}

// GenerateSVGSeries renders an arbitrary number of series over a shared set
// of categories into a self-contained, standalone SVG grouped bar chart,
// matching the visual style of GenerateSVG (log Y-axis, opaque card
// background, system font stack, per-bar value labels, rotated category
// labels). It generalizes GenerateSVG's fixed three-engine layout to any
// series count so other comparisons (e.g. a third-party engine) can reuse
// the same chart look without go-pug's own three-way corpus taking on a new
// dependency.
func GenerateSVGSeries(title, subtitle string, categories []string, series []Series) []byte {
	n := len(categories)
	numSeries := len(series)
	chartHeight := marginTop + plotHeight + marginBottom
	plotWidth := chartWidth - marginLeft - marginRight
	plotBottom := marginTop + plotHeight

	minVal, maxVal := math.Inf(1), math.Inf(-1)
	for _, s := range series {
		for _, v := range s.Values {
			if v <= 0 {
				continue
			}
			if v < minVal {
				minVal = v
			}
			if v > maxVal {
				maxVal = v
			}
		}
	}
	if math.IsInf(minVal, 1) {
		minVal, maxVal = 1, 10
	}
	loPow := int(math.Floor(math.Log10(minVal)))
	hiPow := int(math.Ceil(math.Log10(maxVal)))
	if hiPow == loPow {
		hiPow++
	}
	logRange := float64(hiPow - loPow)

	yFor := func(v float64) float64 {
		if v <= 0 {
			return plotBottom
		}
		frac := (math.Log10(v) - float64(loPow)) / logRange
		if frac < 0 {
			frac = 0
		}
		if frac > 1 {
			frac = 1
		}
		return plotBottom - frac*plotHeight
	}

	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %g %g" width="%g" height="%g" font-family="-apple-system, BlinkMacSystemFont, 'Segoe UI', Helvetica, Arial, sans-serif">`+"\n",
		chartWidth, chartHeight, chartWidth, chartHeight)

	fmt.Fprintf(&b, `<rect x="0" y="0" width="%g" height="%g" rx="12" fill="#f6f8fa" stroke="#d0d7de" stroke-width="1"/>`+"\n", chartWidth, chartHeight)

	fmt.Fprintf(&b, `<text x="%g" y="32" font-size="20" font-weight="700" fill="#1f2328" text-anchor="middle">%s</text>`+"\n", chartWidth/2, htmlEscape(title))
	fmt.Fprintf(&b, `<text x="%g" y="52" font-size="12.5" fill="#57606a" text-anchor="middle">%s</text>`+"\n", chartWidth/2, htmlEscape(subtitle))

	// Legend, centered under the subtitle.
	legendY := 74.0
	swatchW := 14.0
	gapAfterSwatch := 6.0
	gapBetweenEntries := 26.0
	widths := make([]float64, numSeries)
	totalLegendW := 0.0
	for i, s := range series {
		widths[i] = estimateTextWidth(s.Label)
		totalLegendW += swatchW + gapAfterSwatch + widths[i]
		if i > 0 {
			totalLegendW += gapBetweenEntries
		}
	}
	lx := chartWidth/2 - totalLegendW/2
	for i, s := range series {
		fmt.Fprintf(&b, `<rect x="%g" y="%g" width="%g" height="%g" rx="2" fill="%s"/>`+"\n", lx, legendY-11, swatchW, swatchW, s.Color)
		fmt.Fprintf(&b, `<text x="%g" y="%g" font-size="12.5" fill="#1f2328">%s</text>`+"\n", lx+swatchW+gapAfterSwatch, legendY, htmlEscape(s.Label))
		lx += swatchW + gapAfterSwatch + widths[i] + gapBetweenEntries
	}

	// Y-axis: one gridline + tick label per power of ten in range.
	for p := loPow; p <= hiPow; p++ {
		y := plotBottom - float64(p-loPow)/logRange*plotHeight
		fmt.Fprintf(&b, `<line x1="%g" y1="%g" x2="%g" y2="%g" stroke="#d0d7de" stroke-width="1"/>`+"\n", marginLeft, y, marginLeft+plotWidth, y)
		fmt.Fprintf(&b, `<text x="%g" y="%g" font-size="11" fill="#57606a" text-anchor="end">%s</text>`+"\n", marginLeft-8, y+4, formatTick(p))
	}
	fmt.Fprintf(&b, `<text x="18" y="%g" font-size="12.5" fill="#57606a" text-anchor="middle" transform="rotate(-90 18 %g)">renders / second (log scale)</text>`+"\n", plotBottom-plotHeight/2, plotBottom-plotHeight/2)

	// Bars, numSeries per category group. The 0.84-of-groupWidth inner
	// fraction and 4:1 bar:gap ratio reduce to GenerateSVG's exact
	// hardcoded 0.24/0.06 constants when numSeries is 3, keeping this
	// generalization visually consistent with the fixed three-engine chart.
	groupWidth := 0.0
	if n > 0 {
		groupWidth = plotWidth / float64(n)
	}
	const innerFrac = 0.84
	barWidth := 0.0
	barGap := 0.0
	if numSeries > 0 {
		barWidth = innerFrac * groupWidth / (float64(numSeries) + 0.25*float64(numSeries-1))
		barGap = 0.25 * barWidth
	}
	groupInnerWidth := barWidth*float64(numSeries) + barGap*float64(numSeries-1)

	for i, cat := range categories {
		groupX := marginLeft + float64(i)*groupWidth + (groupWidth-groupInnerWidth)/2

		for si, s := range series {
			var v float64
			if i < len(s.Values) {
				v = s.Values[i]
			}
			x := groupX + float64(si)*(barWidth+barGap)
			y := yFor(v)
			h := plotBottom - y
			fmt.Fprintf(&b, `<rect x="%g" y="%g" width="%g" height="%g" fill="%s"/>`+"\n", x, y, barWidth, h, s.Color)
			labelY := y - 5
			if labelY < marginTop-2 {
				labelY = marginTop - 2
			}
			fmt.Fprintf(&b, `<text x="%g" y="%g" font-size="9.5" fill="#1f2328" text-anchor="middle">%s</text>`+"\n", x+barWidth/2, labelY, formatCount(v))
		}

		labelX := groupX + groupInnerWidth/2
		labelY := plotBottom + 16
		fmt.Fprintf(&b, `<text x="%g" y="%g" font-size="12" fill="#1f2328" text-anchor="end" transform="rotate(-30 %g %g)">%s</text>`+"\n",
			labelX, labelY, labelX, labelY, htmlEscape(cat))
	}

	fmt.Fprintf(&b, `<line x1="%g" y1="%g" x2="%g" y2="%g" stroke="#8c959f" stroke-width="1.5"/>`+"\n", marginLeft, plotBottom, marginLeft+plotWidth, plotBottom)

	b.WriteString("</svg>\n")
	return []byte(b.String())
}
