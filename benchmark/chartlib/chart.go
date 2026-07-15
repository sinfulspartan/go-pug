package chartlib

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// engineSeries is one engine's bar color/label, paired with an accessor for
// its value on a TemplateResult so the layout code can iterate the three
// engines uniformly instead of repeating itself per engine.
type engineSeries struct {
	label string
	color string
	value func(TemplateResult) float64
}

var engines = []engineSeries{
	{label: "pug.js (Node)", color: "#d55e00", value: func(t TemplateResult) float64 { return t.PugjsRendersPerSec }},
	{label: "go-pug interpreter", color: "#0072b2", value: func(t TemplateResult) float64 { return t.InterpreterRendersPerSec }},
	{label: "go-pug codegen", color: "#009e73", value: func(t TemplateResult) float64 { return t.CodegenRendersPerSec }},
}

const (
	chartWidth   = 980.0
	marginLeft   = 90.0
	marginRight  = 30.0
	marginTop    = 92.0
	marginBottom = 130.0
	plotHeight   = 420.0
)

// GenerateSVG renders results into a self-contained, standalone SVG grouped
// bar chart: one group of 3 bars (pug.js / interpreter / codegen) per
// template, renders/second on a log Y-axis (chosen over linear because the
// interpreter's throughput sits one to two orders of magnitude below both
// pug.js and codegen on this corpus — a linear axis would flatten the
// interpreter's bars to invisible slivers; every bar still carries its own
// printed value label, so the log axis loses no honesty, only unreadable bar
// heights). The SVG draws its own opaque background so it is legible on both
// GitHub's light and dark theme, and uses only the system font stack (no
// external fonts/CSS).
func GenerateSVG(r Results) []byte {
	n := len(r.Templates)
	chartHeight := marginTop + plotHeight + marginBottom
	plotWidth := chartWidth - marginLeft - marginRight
	plotBottom := marginTop + plotHeight

	minVal, maxVal := math.Inf(1), math.Inf(-1)
	for _, t := range r.Templates {
		for _, e := range engines {
			v := e.value(t)
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

	// Opaque card background so the chart is legible regardless of the
	// surrounding page's light/dark theme.
	fmt.Fprintf(&b, `<rect x="0" y="0" width="%g" height="%g" rx="12" fill="#f6f8fa" stroke="#d0d7de" stroke-width="1"/>`+"\n", chartWidth, chartHeight)

	fmt.Fprintf(&b, `<text x="%g" y="32" font-size="20" font-weight="700" fill="#1f2328" text-anchor="middle">Render throughput &#8212; higher is better</text>`+"\n", chartWidth/2)
	fmt.Fprintf(&b, `<text x="%g" y="52" font-size="12.5" fill="#57606a" text-anchor="middle">renders/second per template, log scale (pug.js 3.0.4 on Node vs go-pug interpreter vs go-pug codegen, compiled Go)</text>`+"\n", chartWidth/2)

	// Legend, centered under the subtitle.
	legendY := 74.0
	swatchW := 14.0
	gapAfterSwatch := 6.0
	gapBetweenEntries := 26.0
	textWidths := []float64{112, 150, 130} // approximate rendered widths for the fixed labels above
	totalLegendW := 0.0
	for i := range engines {
		totalLegendW += swatchW + gapAfterSwatch + textWidths[i]
		if i > 0 {
			totalLegendW += gapBetweenEntries
		}
	}
	lx := chartWidth/2 - totalLegendW/2
	for i, e := range engines {
		fmt.Fprintf(&b, `<rect x="%g" y="%g" width="%g" height="%g" rx="2" fill="%s"/>`+"\n", lx, legendY-11, swatchW, swatchW, e.color)
		fmt.Fprintf(&b, `<text x="%g" y="%g" font-size="12.5" fill="#1f2328">%s</text>`+"\n", lx+swatchW+gapAfterSwatch, legendY, htmlEscape(e.label))
		lx += swatchW + gapAfterSwatch + textWidths[i] + gapBetweenEntries
	}

	// Y-axis: one gridline + tick label per power of ten in range.
	for p := loPow; p <= hiPow; p++ {
		y := plotBottom - float64(p-loPow)/logRange*plotHeight
		fmt.Fprintf(&b, `<line x1="%g" y1="%g" x2="%g" y2="%g" stroke="#d0d7de" stroke-width="1"/>`+"\n", marginLeft, y, marginLeft+plotWidth, y)
		fmt.Fprintf(&b, `<text x="%g" y="%g" font-size="11" fill="#57606a" text-anchor="end">%s</text>`+"\n", marginLeft-8, y+4, formatTick(p))
	}
	fmt.Fprintf(&b, `<text x="18" y="%g" font-size="12.5" fill="#57606a" text-anchor="middle" transform="rotate(-90 18 %g)">renders / second (log scale)</text>`+"\n", plotBottom-plotHeight/2, plotBottom-plotHeight/2)

	// Bars, three per template group.
	groupWidth := plotWidth / float64(n)
	barWidth := groupWidth * 0.24
	barGap := groupWidth * 0.06
	groupInnerWidth := barWidth*3 + barGap*2

	for i, t := range r.Templates {
		groupX := marginLeft + float64(i)*groupWidth + (groupWidth-groupInnerWidth)/2

		for ei, e := range engines {
			v := e.value(t)
			x := groupX + float64(ei)*(barWidth+barGap)
			y := yFor(v)
			h := plotBottom - y
			fmt.Fprintf(&b, `<rect x="%g" y="%g" width="%g" height="%g" fill="%s"/>`+"\n", x, y, barWidth, h, e.color)
			labelY := y - 5
			if labelY < marginTop-2 {
				labelY = marginTop - 2
			}
			fmt.Fprintf(&b, `<text x="%g" y="%g" font-size="9.5" fill="#1f2328" text-anchor="middle">%s</text>`+"\n", x+barWidth/2, labelY, formatCount(v))
		}

		labelX := groupX + groupInnerWidth/2
		labelY := plotBottom + 16
		fmt.Fprintf(&b, `<text x="%g" y="%g" font-size="12" fill="#1f2328" text-anchor="end" transform="rotate(-30 %g %g)">%s</text>`+"\n",
			labelX, labelY, labelX, labelY, htmlEscape(t.Name))
	}

	fmt.Fprintf(&b, `<line x1="%g" y1="%g" x2="%g" y2="%g" stroke="#8c959f" stroke-width="1.5"/>`+"\n", marginLeft, plotBottom, marginLeft+plotWidth, plotBottom)

	b.WriteString("</svg>\n")
	return []byte(b.String())
}

// formatTick renders 10^p as a comma-grouped integer label ("1", "10",
// "1,000", "100,000", ...).
func formatTick(p int) string {
	v := math.Pow(10, float64(p))
	return formatCount(v)
}

// formatCount renders v as a comma-grouped integer, the same style used for
// both axis tick labels and per-bar value labels.
func formatCount(v float64) string {
	n := int64(math.Round(v))
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var out []byte
	for i, c := range []byte(s) {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, c)
	}
	res := string(out)
	if neg {
		res = "-" + res
	}
	return res
}

func htmlEscape(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}
