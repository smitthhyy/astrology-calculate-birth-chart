package wheel

import (
	"encoding/xml"
	"math"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"

	"astronomyCalculator/internal/astro"
)

func testChart(t *testing.T) *astro.Chart {
	t.Helper()
	loc, err := time.LoadLocation("Pacific/Honolulu")
	if err != nil {
		t.Fatalf("loading zone: %v", err)
	}
	c, err := astro.Compute(astro.Birth{
		Name: "Reference", Year: 1961, Month: time.August, Day: 4, Hour: 19, Minute: 24,
		Zone: loc, ZoneName: "Pacific/Honolulu",
		Place: "Honolulu", Country: "United States",
		Latitude: 21.306944, Longitude: -157.858333,
	})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	return c
}

func TestRenderProducesWellFormedSVG(t *testing.T) {
	svg := Render(testChart(t), Options{Title: "Reference", Subtitle: "Honolulu"})

	if !strings.HasPrefix(svg, "<svg ") {
		t.Fatalf("output does not start with an svg element: %.60q", svg)
	}
	// Parsing the whole document catches unbalanced tags and bad attributes,
	// which a browser would silently render as a blank box.
	if err := xml.Unmarshal([]byte(svg), new(struct {
		XMLName xml.Name
	})); err != nil {
		t.Fatalf("output is not well-formed XML: %v", err)
	}
	if strings.Contains(svg, "NaN") || strings.Contains(svg, "Inf") {
		t.Error("output contains a non-finite coordinate")
	}
}

func TestRenderStandaloneAddsXMLDeclaration(t *testing.T) {
	svg := Render(testChart(t), Options{Standalone: true})
	if !strings.HasPrefix(svg, `<?xml version="1.0" encoding="UTF-8"?>`) {
		t.Error("standalone output lacks an XML declaration")
	}
	if err := xml.Unmarshal([]byte(svg), new(struct{ XMLName xml.Name })); err != nil {
		t.Fatalf("standalone output is not well-formed: %v", err)
	}
}

func TestRenderEscapesTitles(t *testing.T) {
	svg := Render(testChart(t), Options{Title: `<script>alert("x")</script>`})
	if strings.Contains(svg, "<script>") {
		t.Error("the title was not escaped")
	}
	if !strings.Contains(svg, "&lt;script&gt;") {
		t.Error("expected the escaped title in the output")
	}
}

func TestRenderIncludesEveryDrawnElement(t *testing.T) {
	c := testChart(t)
	svg := Render(c, Options{})

	for _, want := range []string{"AC", "DC", "MC", "IC", "Fire", "Earth", "Air", "Water",
		"Trine", "Sextile", "Opposition", "Square"} {
		if !strings.Contains(svg, ">"+want+"<") {
			t.Errorf("label %q is missing from the wheel", want)
		}
	}
	for i := 0; i < 12; i++ {
		if g := astro.Sign(i).Glyph(); !strings.Contains(svg, g) {
			t.Errorf("sign glyph %q (%s) is missing", g, astro.Sign(i))
		}
	}
	for _, body := range astro.WheelBodies {
		if !strings.Contains(svg, body.Glyph()) {
			t.Errorf("body glyph for %s is missing", body)
		}
	}
}

func TestPointPlacesTheAscendantOnTheLeft(t *testing.T) {
	const asc = 137.5
	x, y := point(asc, asc, rOuter)
	if math.Abs(x-(cx-rOuter)) > 1e-9 || math.Abs(y-cy) > 1e-9 {
		t.Errorf("ascendant drawn at (%.3f, %.3f), want (%.3f, %.3f)", x, y, cx-rOuter, cy)
	}
	// A quarter of the zodiac later should be at the bottom of the wheel,
	// which is where the fourth house cusp belongs.
	x, y = point(asc+90, asc, rOuter)
	if math.Abs(x-cx) > 1e-9 || math.Abs(y-(cy+rOuter)) > 1e-9 {
		t.Errorf("ascendant+90° drawn at (%.3f, %.3f), want (%.3f, %.3f)", x, y, cx, cy+rOuter)
	}
}

func TestSpreadSeparatesClusteredGlyphsAndKeepsOrder(t *testing.T) {
	glyphs := []placed{
		{body: astro.Sun, true_: 100.0, display: 100.0},
		{body: astro.Mercury, true_: 101.0, display: 101.0},
		{body: astro.Venus, true_: 102.5, display: 102.5},
		{body: astro.Mars, true_: 250.0, display: 250.0},
	}
	spread(glyphs)

	for i := range glyphs {
		j := (i + 1) % len(glyphs)
		gap := math.Mod(glyphs[j].display-glyphs[i].display+360, 360)
		if gap+1e-9 < minGlyphSeparation {
			t.Errorf("%s and %s are only %.3f° apart, want at least %.1f°",
				glyphs[i].body, glyphs[j].body, gap, minGlyphSeparation)
		}
	}

	// Order around the wheel must be preserved, and true longitudes untouched.
	order := make([]astro.Body, len(glyphs))
	for i, g := range glyphs {
		order[i] = g.body
	}
	want := []astro.Body{astro.Sun, astro.Mercury, astro.Venus, astro.Mars}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("glyph order is %v, want %v", order, want)
		}
	}
	// The cluster should stay centred on where it actually is.
	mid := (glyphs[0].display + glyphs[2].display) / 2
	if math.Abs(mid-101.25) > 1e-9 {
		t.Errorf("cluster recentred to %.3f°, want 101.25°", mid)
	}
}

func TestSpreadLeavesWellSeparatedGlyphsAlone(t *testing.T) {
	glyphs := []placed{
		{body: astro.Sun, true_: 10, display: 10},
		{body: astro.Moon, true_: 130, display: 130},
		{body: astro.Mars, true_: 250, display: 250},
	}
	spread(glyphs)
	for _, g := range glyphs {
		if math.Abs(g.display-g.true_) > 1e-9 {
			t.Errorf("%s moved from %.3f° to %.3f° with no crowding", g.body, g.true_, g.display)
		}
	}
}
