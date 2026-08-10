// Package wheel draws a natal chart as an SVG chart wheel.
//
// The output is a single self-contained <svg> element with no external
// references, so the same markup works inlined in a page, saved as a .svg
// file, or rasterised to PNG in the browser.
package wheel

import (
	"fmt"
	"html"
	"math"
	"sort"
	"strings"

	"astronomyCalculator/internal/astro"
)

// Geometry, in user units. The viewBox is 1000 wide, so these are effectively
// per-mille of the rendered width and the whole drawing scales to any size.
const (
	width  = 1000.0
	height = 1200.0

	cx = 500.0
	cy = 600.0

	rOuter    = 430.0 // outer edge of the zodiac ring
	rZodiacIn = 372.0 // inner edge of the zodiac ring
	rHouseIn  = 322.0 // inner edge of the house-number band
	rGlyph    = 290.0 // where planet glyphs sit
	rAspect   = 248.0 // circle the aspect chords are drawn on

	rSignGlyph  = (rOuter + rZodiacIn) / 2
	rHouseLabel = (rZodiacIn + rHouseIn) / 2
	rAngleLabel = rOuter + 28 // AC/DC/MC/IC, just clear of the rim

	// A planet glyph needs about this much arc to itself before it collides
	// with its neighbour.
	minGlyphSeparation = 9.0 // degrees
)

// The palette is the validated instance from the project's data-visualisation
// reference: four categorical hues for the elements and two for the aspect
// families, checked with scripts/validate_palette.js against the light chart
// surface.
//
// The wheel is deliberately fixed to that light surface rather than following
// the page theme. It is a document — it gets downloaded, shared and printed —
// and the warm/cool element hues have no dark-surface stepping that clears the
// normal-vision separation floor, so a themed version would have to be worse.
const (
	surface     = "#fcfcfb"
	inkPrimary  = "#0b0b0b"
	inkMuted    = "#898781"
	hairline    = "#e1e0d9"
	axisLine    = "#c3c2b7"
	harmonious  = "#2a78d6" // trine and sextile
	challenging = "#e34948" // square and opposition
)

// elementColour maps a sign's element to its hue. Colour only reinforces here:
// every sector also carries its glyph in ink and is separated from its
// neighbours by a gap, and the sign is named in full in the tables, so nothing
// depends on telling two hues apart.
var elementColour = map[string]string{
	"Fire":  "#e34948",
	"Earth": "#008300",
	"Air":   "#eda100",
	"Water": "#2a78d6",
}

// Options control the text drawn around the wheel.
type Options struct {
	Title    string
	Subtitle string
	// Standalone prepends an XML declaration, for serving the wheel as a
	// .svg file rather than inlining it.
	Standalone bool
}

// Render draws the chart and returns the SVG source.
func Render(c *astro.Chart, opt Options) string {
	var b strings.Builder
	if opt.Standalone {
		b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` + "\n")
	}
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %g %g" `+
		`width="100%%" role="img" aria-label="Birth chart wheel" class="chart-wheel">`, width, height)

	b.WriteString(styleBlock())
	fmt.Fprintf(&b, `<rect width="%g" height="%g" fill="%s"/>`, width, height, surface)

	writeHeading(&b, opt)

	// The Ascendant is pinned to the left of the wheel, the convention every
	// chart is read by, and longitude increases anticlockwise from there.
	asc := c.Houses.Ascendant

	writeZodiacRing(&b, asc)
	writeDegreeTicks(&b, asc)
	writeHouses(&b, c, asc)
	writeAspects(&b, c, asc)
	writeAngles(&b, c, asc)
	writePlanets(&b, c, asc)
	writeLegend(&b)

	b.WriteString(`</svg>`)
	return b.String()
}

func styleBlock() string {
	return `<style>
    .chart-wheel text { font-family: system-ui, -apple-system, "Segoe UI", sans-serif; }
    .chart-wheel .cw-sign, .chart-wheel .cw-body { font-variant-emoji: text; }
    .cw-sign  { font-size: 38px; fill: ` + inkPrimary + `; }
    .cw-body  { font-size: 36px; fill: ` + inkPrimary + `; }
    .cw-house { font-size: 24px; fill: ` + inkMuted + `; }
    .cw-angle { font-size: 26px; font-weight: 600; fill: ` + inkPrimary + `; }
    .cw-title { font-size: 36px; font-weight: 600; fill: ` + inkPrimary + `; }
    .cw-sub   { font-size: 24px; fill: #52514e; }
    .cw-key   { font-size: 22px; fill: #52514e; }
    .cw-rx    { font-size: 17px; fill: ` + inkMuted + `; }
  </style>`
}

// point converts an ecliptic longitude to a position on the wheel. The
// Ascendant sits at the nine o'clock position and longitude increases
// anticlockwise, so the tenth house cusp lands near the top.
func point(lon, asc, r float64) (x, y float64) {
	θ := (180 + lon - asc) * math.Pi / 180
	return cx + r*math.Cos(θ), cy - r*math.Sin(θ)
}

// arcPath draws the ring segment between two longitudes at two radii.
func arcPath(from, to, asc, rIn, rOut float64) string {
	x1, y1 := point(from, asc, rOut)
	x2, y2 := point(to, asc, rOut)
	x3, y3 := point(to, asc, rIn)
	x4, y4 := point(from, asc, rIn)

	large := "0"
	if math.Mod(to-from+360, 360) > 180 {
		large = "1"
	}
	// Longitude increases anticlockwise on screen, which is the negative
	// sweep direction for an SVG arc.
	return fmt.Sprintf("M %.2f %.2f A %.2f %.2f 0 %s 0 %.2f %.2f L %.2f %.2f A %.2f %.2f 0 %s 1 %.2f %.2f Z",
		x1, y1, rOut, rOut, large, x2, y2,
		x3, y3, rIn, rIn, large, x4, y4)
}

func line(b *strings.Builder, x1, y1, x2, y2 float64, stroke string, w float64, extra string) {
	fmt.Fprintf(b, `<line x1="%.2f" y1="%.2f" x2="%.2f" y2="%.2f" stroke="%s" stroke-width="%.1f"%s/>`,
		x1, y1, x2, y2, stroke, w, extra)
}

func text(b *strings.Builder, x, y float64, class, anchor, s string) {
	fmt.Fprintf(b, `<text x="%.2f" y="%.2f" class="%s" text-anchor="%s" dominant-baseline="central">%s</text>`,
		x, y, class, anchor, html.EscapeString(s))
}

// textPresentation is U+FE0E VARIATION SELECTOR-15. The zodiac and planet
// symbols have emoji forms, and browsers will happily draw ♈ as a coloured
// sticker; this suffix asks for the plain typographic glyph instead. The CSS
// font-variant-emoji property does the same job on newer engines and is set in
// the style block as well, since neither is universally supported.
const textPresentation = "︎"

// glyph renders an astrological symbol as text rather than as emoji.
func glyph(b *strings.Builder, x, y float64, class, s string) {
	text(b, x, y, class, "middle", s+textPresentation)
}

func writeHeading(b *strings.Builder, opt Options) {
	if opt.Title != "" {
		text(b, cx, 40, "cw-title", "middle", opt.Title)
	}
	if opt.Subtitle != "" {
		text(b, cx, 82, "cw-sub", "middle", opt.Subtitle)
	}
}

func writeZodiacRing(b *strings.Builder, asc float64) {
	for i := 0; i < 12; i++ {
		sign := astro.Sign(i)
		start, end := float64(i*30), float64(i*30+30)

		// A small inset at each end leaves the 2px gap between neighbouring
		// fills that keeps the boundary readable without a heavy divider.
		const gap = 0.25 // degrees, ≈2px at this radius
		fmt.Fprintf(b, `<path d="%s" fill="%s" fill-opacity="0.13"/>`,
			arcPath(start+gap, end-gap, asc, rZodiacIn, rOuter), elementColour[sign.Element()])

		x, y := point(start+15, asc, rSignGlyph)
		glyph(b, x, y, "cw-sign", sign.Glyph())

		// Sector divider.
		x1, y1 := point(start, asc, rZodiacIn)
		x2, y2 := point(start, asc, rOuter)
		line(b, x1, y1, x2, y2, hairline, 2, "")
	}
	circle(b, rOuter, axisLine, 2)
	circle(b, rZodiacIn, axisLine, 2)
	circle(b, rHouseIn, hairline, 2)
	circle(b, rAspect, hairline, 2)
}

func circle(b *strings.Builder, r float64, stroke string, w float64) {
	fmt.Fprintf(b, `<circle cx="%g" cy="%g" r="%.1f" fill="none" stroke="%s" stroke-width="%.1f"/>`,
		cx, cy, r, stroke, w)
}

// writeDegreeTicks marks the ecliptic in five- and ten-degree steps along the
// inner edge of the zodiac ring, so a placement can be read off the wheel.
func writeDegreeTicks(b *strings.Builder, asc float64) {
	for d := 0; d < 360; d += 5 {
		length := 8.0
		if d%10 == 0 {
			length = 14
		}
		if d%30 == 0 {
			continue // the sector dividers already mark these
		}
		x1, y1 := point(float64(d), asc, rZodiacIn)
		x2, y2 := point(float64(d), asc, rZodiacIn+length)
		line(b, x1, y1, x2, y2, hairline, 1.5, "")
	}
}

func writeHouses(b *strings.Builder, c *astro.Chart, asc float64) {
	cusps := c.Houses.Cusps
	for i := 0; i < 12; i++ {
		cusp := cusps[i]
		isAngle := i == 0 || i == 3 || i == 6 || i == 9

		stroke, w, dash := hairline, 2.0, ` stroke-dasharray="6 6"`
		if isAngle {
			stroke, w, dash = axisLine, 3.0, ""
		}
		x1, y1 := point(cusp, asc, rAspect)
		x2, y2 := point(cusp, asc, rZodiacIn)
		line(b, x1, y1, x2, y2, stroke, w, dash)

		// The number goes in the middle of the house it opens.
		span := math.Mod(cusps[(i+1)%12]-cusp+360, 360)
		x, y := point(cusp+span/2, asc, rHouseLabel)
		text(b, x, y, "cw-house", "middle", fmt.Sprint(i+1))
	}
}

// writeAngles labels the four cardinal points just outside the wheel.
func writeAngles(b *strings.Builder, c *astro.Chart, asc float64) {
	for _, a := range []struct {
		lon   float64
		label string
	}{
		{c.Houses.Ascendant, "AC"},
		{c.Houses.Descendant(), "DC"},
		{c.Houses.Midheaven, "MC"},
		{c.Houses.ImumCoeli(), "IC"},
	} {
		x, y := point(a.lon, asc, rAngleLabel)
		text(b, x, y, "cw-angle", "middle", a.label)
	}
}

// writeAspects draws the chords across the middle of the wheel. Conjunctions
// are left out on purpose: two bodies at the same longitude produce a stub of
// a line that says nothing the adjacent glyphs do not already show.
func writeAspects(b *strings.Builder, c *astro.Chart, asc float64) {
	for _, a := range c.Aspects {
		var stroke, dash string
		switch a.Name {
		case "Trine":
			stroke, dash = harmonious, ""
		case "Sextile":
			stroke, dash = harmonious, ` stroke-dasharray="10 8"`
		case "Opposition":
			stroke, dash = challenging, ""
		case "Square":
			stroke, dash = challenging, ` stroke-dasharray="10 8"`
		default:
			continue
		}

		pa, okA := c.Placement(a.A)
		pb, okB := c.Placement(a.B)
		if !okA || !okB {
			continue
		}
		x1, y1 := point(pa.Longitude, asc, rAspect)
		x2, y2 := point(pb.Longitude, asc, rAspect)
		// Tighter aspects are drawn more strongly, so the structural ones
		// stand out from the incidental.
		opacity := 0.35 + 0.5*(1-math.Min(a.Orb/8, 1))
		line(b, x1, y1, x2, y2, stroke, 2,
			fmt.Sprintf(`%s stroke-opacity="%.2f" stroke-linecap="round"`, dash, opacity))
	}
}

// placed is a glyph after collision spreading: it keeps the true longitude for
// the pointer and carries a possibly nudged angle for the glyph itself.
type placed struct {
	body    astro.Body
	true_   float64
	display float64
	retro   bool
}

func writePlanets(b *strings.Builder, c *astro.Chart, asc float64) {
	var glyphs []placed
	for _, body := range astro.WheelBodies {
		p, ok := c.Placement(body)
		if !ok {
			continue
		}
		glyphs = append(glyphs, placed{body: body, true_: p.Longitude, display: p.Longitude, retro: p.Retrograde})
	}
	spread(glyphs)

	for _, g := range glyphs {
		// A tick at the exact degree, then a leader to the glyph. Without
		// this a nudged glyph would misreport its own position.
		tx1, ty1 := point(g.true_, asc, rZodiacIn)
		tx2, ty2 := point(g.true_, asc, rZodiacIn-16)
		line(b, tx1, ty1, tx2, ty2, inkMuted, 2, "")

		lx, ly := point(g.display, asc, rGlyph+30)
		line(b, tx2, ty2, lx, ly, hairline, 1.5, "")

		x, y := point(g.display, asc, rGlyph)
		// A ring of the surface colour behind each glyph keeps it legible
		// where an aspect line or a cusp passes underneath.
		fmt.Fprintf(b, `<circle cx="%.2f" cy="%.2f" r="25" fill="%s"/>`, x, y, surface)
		glyph(b, x, y, "cw-body", g.body.Glyph())

		if g.retro {
			rx, ry := point(g.display, asc, rGlyph-30)
			text(b, rx, ry, "cw-rx", "middle", "℞")
		}
	}
}

// spread nudges glyphs apart so that a stellium stays readable, keeping them
// in zodiacal order and centred on the group they belong to.
func spread(glyphs []placed) {
	if len(glyphs) < 2 {
		return
	}
	sort.Slice(glyphs, func(i, j int) bool { return glyphs[i].true_ < glyphs[j].true_ })

	// Rotate so the widest gap falls at the seam; the run of glyphs can then
	// be treated as a line rather than a circle.
	widest, at := -1.0, 0
	for i := range glyphs {
		next := glyphs[(i+1)%len(glyphs)].true_
		if gap := math.Mod(next-glyphs[i].true_+360, 360); gap > widest {
			widest, at = gap, (i+1)%len(glyphs)
		}
	}
	ordered := append(append([]placed{}, glyphs[at:]...), glyphs[:at]...)

	// Unroll to a monotonically increasing sequence.
	pos := make([]float64, len(ordered))
	pos[0] = ordered[0].true_
	for i := 1; i < len(ordered); i++ {
		pos[i] = pos[i-1] + math.Mod(ordered[i].true_-ordered[i-1].true_+360, 360)
	}

	// Find runs that are too tightly packed and space each one evenly about
	// its own midpoint.
	for start := 0; start < len(pos); {
		end := start
		for end+1 < len(pos) && pos[end+1]-pos[end] < minGlyphSeparation {
			end++
		}
		if end > start {
			n := end - start + 1
			mid := (pos[start] + pos[end]) / 2
			first := mid - minGlyphSeparation*float64(n-1)/2
			for i := 0; i < n; i++ {
				pos[start+i] = first + minGlyphSeparation*float64(i)
			}
		}
		start = end + 1
	}

	for i, p := range ordered {
		p.display = math.Mod(pos[i]+360, 360)
		ordered[i] = p
	}
	copy(glyphs, ordered)
}

// writeLegend spells out both colour codings underneath the wheel, so neither
// the elements nor the aspect families rely on hue alone.
func writeLegend(b *strings.Builder) {
	const (
		rowA = 1112.0
		rowB = 1162.0
	)

	x := 96.0
	for _, e := range []string{"Fire", "Earth", "Air", "Water"} {
		fmt.Fprintf(b, `<rect x="%.1f" y="%.1f" width="20" height="20" rx="4" fill="%s" fill-opacity="0.35" stroke="%s" stroke-width="2"/>`,
			x, rowA-10, elementColour[e], elementColour[e])
		text(b, x+30, rowA, "cw-key", "start", e)
		x += 200
	}

	x = 96.0
	for _, a := range []struct {
		name, colour, dash string
	}{
		{"Trine", harmonious, ""},
		{"Sextile", harmonious, ` stroke-dasharray="10 8"`},
		{"Opposition", challenging, ""},
		{"Square", challenging, ` stroke-dasharray="10 8"`},
	} {
		line(b, x, rowB, x+40, rowB, a.colour, 3, a.dash+` stroke-linecap="round"`)
		text(b, x+50, rowB, "cw-key", "start", a.name)
		x += 200
	}
}
