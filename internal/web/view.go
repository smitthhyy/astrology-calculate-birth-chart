package web

import (
	"fmt"
	"html/template"
	"strings"

	"astronomyCalculator/internal/astro"
	"astronomyCalculator/internal/interp"
	"astronomyCalculator/internal/wheel"
)

// Row is one line of the main table, in the order the columns are asked for:
// Cosmic Part, Zodiac sign, What it defines, Definition, How it shows up.
type Row struct {
	CosmicPart string
	Glyph      string
	Sign       string // e.g. "12°32' Leo"
	SignName   string
	House      int
	Retrograde bool
	Defines    string
	Definition string
	ShowsUp    string
}

// PointRow is one of the secondary chart points, which carry a placement but
// no per-sign reading.
type PointRow struct {
	Name    string
	Glyph   string
	Sign    string
	House   int
	Defines string
	ShowsUp string
}

// HouseRow is one house, ready for the houses table.
type HouseRow struct {
	Number    int
	Cusp      string
	Sign      string
	Meaning   string
	ShowsUp   string
	Occupants string
}

// AspectRow is one aspect between two chart points.
type AspectRow struct {
	Between string
	Aspect  string
	Glyph   string
	Orb     string
	ShowsUp string
	Family  string // "harmonious", "challenging" or "conjunction"
}

// Result is everything the result template needs.
type Result struct {
	Input   Input
	Chart   *astro.Chart
	Rows    []Row
	Points  []PointRow
	Houses  []HouseRow
	Aspects []AspectRow
	Wheel   template.HTML
	// Query re-encodes the birth details, for the links that recast the same
	// chart in another form — the form, the standalone SVG, the JSON export.
	Query template.URL

	Heading string
	BornOn  string
	BornAt  string
	BornIn  string
	Zone    string
	UTC     string
	System  string

	// Warnings are the things that could make the chart wrong, shown at the top
	// of the page where they cannot be missed. Notes are the things that are
	// merely worth knowing — a planet a degree from a cusp, an aspect at the
	// edge of its orb — and are tucked away, because a page of alert boxes
	// teaches the reader to ignore alert boxes.
	Warnings []astro.Warning
	Notes    []astro.Warning
}

func buildResult(in Input, c *astro.Chart) Result {
	res := Result{
		Input:  in,
		Chart:  c,
		Query:  template.URL(in.Query()),
		System: string(c.Houses.System),
	}
	for _, w := range c.Warnings {
		if w.Severity == astro.SeverityInfo {
			res.Notes = append(res.Notes, w)
		} else {
			res.Warnings = append(res.Warnings, w)
		}
	}

	res.Heading = "Birth chart"
	if c.Birth.Name != "" {
		res.Heading = c.Birth.Name + "'s birth chart"
	}
	res.BornOn = c.Local.Format("2 January 2006")
	res.BornAt = c.Local.Format("15:04")
	res.BornIn = strings.Join(nonEmpty(c.Birth.Place, c.Birth.Country), ", ")
	res.Zone = describeZone(c)
	res.UTC = fmt.Sprintf("%s UTC · %.4f°%s, %.4f°%s",
		c.UTC.Format("2 Jan 2006 15:04"),
		abs(c.Birth.Latitude), northSouth(c.Birth.Latitude),
		abs(c.Birth.Longitude), eastWest(c.Birth.Longitude))

	for _, body := range astro.TableBodies {
		p, ok := c.Placement(body)
		if !ok {
			continue
		}
		row := Row{
			CosmicPart: body.Name(),
			Glyph:      body.Glyph(),
			Sign:       p.Position(),
			SignName:   p.Sign.String(),
			House:      p.House,
			Retrograde: p.Retrograde,
		}
		if b, reading, found := interp.Lookup(body.Key(), p.Sign.Key()); found {
			row.Defines = b.Defines
			row.Definition = reading.Definition
			row.ShowsUp = reading.ShowsUp
		}
		res.Rows = append(res.Rows, row)
	}

	for _, body := range astro.PointBodies {
		p, ok := c.Placement(body)
		if !ok {
			continue
		}
		defines, _ := interp.PointDefines(body.Key())
		showsUp, _ := interp.PointReading(body.Key(), p.Sign.Key())
		res.Points = append(res.Points, PointRow{
			Name:    body.Name(),
			Glyph:   body.Glyph(),
			Sign:    p.Position(),
			House:   p.House,
			Defines: defines,
			ShowsUp: showsUp,
		})
	}

	for _, h := range c.Houses12 {
		names := make([]string, 0, len(h.Occupants))
		for _, b := range h.Occupants {
			names = append(names, b.Glyph()+" "+b.Name())
		}
		occupants := strings.Join(names, ", ")
		if occupants == "" {
			occupants = "—"
		}
		showsUp, _ := interp.HouseReading(h.Number, h.Sign.Key())
		res.Houses = append(res.Houses, HouseRow{
			Number:    h.Number,
			Cusp:      h.Degrees,
			Sign:      h.Sign.String(),
			Meaning:   h.Meaning,
			ShowsUp:   showsUp,
			Occupants: occupants,
		})
	}

	for _, a := range c.Aspects {
		showsUp, _ := interp.AspectReading(a.A.Key(), a.B.Key(), a.Name)
		res.Aspects = append(res.Aspects, AspectRow{
			Between: a.A.Name() + " – " + a.B.Name(),
			Aspect:  a.Name,
			Glyph:   a.Glyph,
			Orb:     a.Exactness(),
			ShowsUp: showsUp,
			Family:  a.Nature(),
		})
	}

	res.Wheel = template.HTML(wheel.Render(c, wheel.Options{
		Title:    c.Birth.Name,
		Subtitle: strings.Join(nonEmpty(res.BornOn+", "+c.Local.Format("15:04"), res.BornIn), " · "),
	}))

	return res
}

// describeZone spells out the offset the chart was actually cast with, and
// whether daylight saving was in force on the day.
//
// This is the one input a reader cannot verify by looking at the output: an
// hour's error moves the Ascendant about fifteen degrees and every house cusp
// with it, but leaves the planets looking almost unchanged. Stating the offset
// and the daylight-saving decision makes it checkable at a glance.
func describeZone(c *astro.Chart) string {
	abbrev, offset := c.Local.Zone()

	sign := "+"
	if offset < 0 {
		sign, offset = "-", -offset
	}
	out := fmt.Sprintf("%s — UTC%s%02d:%02d", c.Birth.ZoneName, sign, offset/3600, (offset%3600)/60)
	if abbrev != "" && !strings.HasPrefix(abbrev, "+") && !strings.HasPrefix(abbrev, "-") {
		out += " (" + abbrev + ")"
	}
	if c.Local.IsDST() {
		return out + ", daylight saving in force"
	}
	return out + ", no daylight saving"
}

func nonEmpty(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			out = append(out, v)
		}
	}
	return out
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func northSouth(lat float64) string {
	if lat < 0 {
		return "S"
	}
	return "N"
}

func eastWest(lon float64) string {
	if lon < 0 {
		return "W"
	}
	return "E"
}
