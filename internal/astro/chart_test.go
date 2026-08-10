package astro

import (
	"math"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"
)

func mustZone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatalf("loading %s: %v", name, err)
	}
	return loc
}

// testChart is the same Honolulu birth the golden chart pins, for the tests
// that only need a real chart to work on.
func testChart(t *testing.T) *Chart {
	t.Helper()
	chart, err := Compute(Birth{
		Name: "Reference",
		Year: 1961, Month: time.August, Day: 4, Hour: 19, Minute: 24,
		Zone: mustZone(t, "Pacific/Honolulu"), ZoneName: "Pacific/Honolulu",
		Place: "Honolulu", Country: "United States",
		Latitude: 21.306944, Longitude: -157.858333,
	})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	return chart
}

// TestComputeGoldenChart pins a whole chart against Swiss Ephemeris output for
// the same moment: 1961-08-04 19:24 in Pacific/Honolulu, at Honolulu.
func TestComputeGoldenChart(t *testing.T) {
	chart, err := Compute(Birth{
		Name: "Reference",
		Year: 1961, Month: time.August, Day: 4, Hour: 19, Minute: 24,
		Zone: mustZone(t, "Pacific/Honolulu"), ZoneName: "Pacific/Honolulu",
		Place: "Honolulu", Country: "United States",
		Latitude: 21.306944, Longitude: -157.858333,
	})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	if got, want := chart.UTC.Format("2006-01-02 15:04"), "1961-08-05 05:24"; got != want {
		t.Errorf("UTC = %s, want %s", got, want)
	}
	if chart.Houses.System != Placidus {
		t.Errorf("house system = %s, want %s", chart.Houses.System, Placidus)
	}
	// Notes about marginal aspects and near-cusp placements are expected of any
	// chart; nothing here should be doubtful enough to warn about.
	for _, w := range chart.Warnings {
		if w.Severity != SeverityInfo {
			t.Errorf("unexpected warning: %+v", w)
		}
	}

	want := map[Body]struct {
		position   string
		house      int
		retrograde bool
	}{
		Sun:        {"12°32' Leo", 6, false},
		Moon:       {"3°21' Gemini", 4, false},
		Mercury:    {"2°19' Leo", 6, false},
		Venus:      {"1°47' Cancer", 5, false},
		Mars:       {"22°34' Virgo", 7, false},
		Jupiter:    {"0°51' Aquarius", 12, true},
		Saturn:     {"25°19' Capricorn", 12, true},
		Uranus:     {"25°16' Leo", 7, false},
		Neptune:    {"8°36' Scorpio", 9, false},
		Pluto:      {"6°58' Virgo", 7, false},
		Ascendant:  {"18°03' Aquarius", 1, false},
		Descendant: {"18°03' Leo", 7, false},
		Midheaven:  {"28°53' Scorpio", 10, false},
		NorthNode:  {"27°17' Leo", 7, true},
	}

	for body, w := range want {
		p, ok := chart.Placement(body)
		if !ok {
			t.Errorf("%s: missing from chart", body)
			continue
		}
		if p.Position() != w.position {
			t.Errorf("%s: position %s, want %s", body, p.Position(), w.position)
		}
		if p.House != w.house {
			t.Errorf("%s: house %d, want %d", body, p.House, w.house)
		}
		if p.Retrograde != w.retrograde {
			t.Errorf("%s: retrograde %v, want %v", body, p.Retrograde, w.retrograde)
		}
	}

	if len(chart.Houses12) != 12 {
		t.Fatalf("got %d houses, want 12", len(chart.Houses12))
	}
	// Every non-angle body has to land in exactly one house.
	placed := 0
	for _, h := range chart.Houses12 {
		placed += len(h.Occupants)
	}
	if want := len(TableBodies) + len(PointBodies) - 4; placed != want {
		t.Errorf("%d bodies placed in houses, want %d", placed, want)
	}

	// The Sun and Neptune are 86°04' apart: a square, 3°56' from exact.
	if !hasAspect(chart, Sun, Neptune, "Square") {
		t.Error("expected a Sun–Neptune square")
	}
	// The Sun and Mercury are 10°13' apart, just outside the 10° orb a
	// conjunction gets when a luminary is involved.
	if hasAspect(chart, Sun, Mercury, "Conjunction") {
		t.Error("Sun–Mercury is outside orb and should not be reported")
	}
}

func hasAspect(c *Chart, a, b Body, name string) bool {
	for _, asp := range c.Aspects {
		if asp.Name != name {
			continue
		}
		if (asp.A == a && asp.B == b) || (asp.A == b && asp.B == a) {
			return true
		}
	}
	return false
}

func TestResolveLocal(t *testing.T) {
	london := mustZone(t, "Europe/London")
	newYork := mustZone(t, "America/New_York")

	tests := []struct {
		name        string
		year        int
		month       time.Month
		day         int
		hour, min   int
		loc         *time.Location
		wantUTC     string
		wantWarning bool
	}{
		{
			name: "ordinary winter time", year: 2000, month: time.January, day: 15,
			hour: 9, min: 30, loc: london, wantUTC: "2000-01-15 09:30",
		},
		{
			name: "ordinary summer time", year: 2000, month: time.July, day: 15,
			hour: 9, min: 30, loc: london, wantUTC: "2000-07-15 08:30",
		},
		{
			// 02:30 on 2023-03-12 never happened in New York.
			name: "clocks forward, time does not exist", year: 2023, month: time.March, day: 12,
			hour: 2, min: 30, loc: newYork, wantWarning: true,
		},
		{
			// 01:30 on 2023-11-05 happened twice; the first is EDT (UTC-4).
			name: "clocks back, time happens twice", year: 2023, month: time.November, day: 5,
			hour: 1, min: 30, loc: newYork, wantUTC: "2023-11-05 05:30", wantWarning: true,
		},
		{
			// Pre-standard-time Berlin ran on local mean time, +0:53:28.
			name: "historical local mean time", year: 1879, month: time.March, day: 14,
			hour: 11, min: 30, loc: mustZone(t, "Europe/Berlin"), wantUTC: "1879-03-14 10:36",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, warning := ResolveLocal(tc.year, tc.month, tc.day, tc.hour, tc.min, tc.loc)
			if tc.wantUTC != "" {
				if u := got.UTC().Format("2006-01-02 15:04"); u != tc.wantUTC {
					t.Errorf("UTC = %s, want %s", u, tc.wantUTC)
				}
			}
			if (warning != nil) != tc.wantWarning {
				t.Errorf("warning = %v, wanted one: %v", warning, tc.wantWarning)
			}
			if warning != nil && (warning.Code == "" || warning.Severity == "" || warning.Message == "") {
				t.Errorf("warning is missing a field: %+v", warning)
			}
		})
	}
}

func TestComputeRejectsBadInput(t *testing.T) {
	base := Birth{
		Year: 1990, Month: time.June, Day: 15, Hour: 14, Minute: 30,
		Zone: time.UTC, Latitude: 10, Longitude: 10,
	}
	t.Run("missing zone", func(t *testing.T) {
		b := base
		b.Zone = nil
		if _, err := Compute(b); err == nil {
			t.Error("expected an error")
		}
	})
	t.Run("latitude out of range", func(t *testing.T) {
		b := base
		b.Latitude = 91
		if _, err := Compute(b); err == nil {
			t.Error("expected an error")
		}
	})
	t.Run("longitude out of range", func(t *testing.T) {
		b := base
		b.Longitude = -181
		if _, err := Compute(b); err == nil {
			t.Error("expected an error")
		}
	})
}

// The almanac name and the illuminated fraction come from the same elongation
// by different routes, so a mismatch between them is a real error rather than
// a matter of taste. An even eighth-of-the-cycle division fails this: it calls
// a 61%-lit Moon the last quarter.
func TestMoonPhaseNameAgreesWithIllumination(t *testing.T) {
	for angle := 0.0; angle < 360; angle += 0.25 {
		name := moonPhaseName(angle)
		lit := (1 - math.Cos(angle*deg2rad)) / 2

		switch name {
		case "New Moon":
			if lit > 0.01 {
				t.Errorf("%.2f°: %s at %.3f lit", angle, name, lit)
			}
		case "Full Moon":
			if lit < 0.99 {
				t.Errorf("%.2f°: %s at %.3f lit", angle, name, lit)
			}
		case "First Quarter", "Last Quarter":
			if math.Abs(lit-0.5) > 0.06 {
				t.Errorf("%.2f°: %s at %.3f lit", angle, name, lit)
			}
		case "Waxing Crescent", "Waning Crescent":
			if lit > 0.5 {
				t.Errorf("%.2f°: %s at %.3f lit", angle, name, lit)
			}
		case "Waxing Gibbous", "Waning Gibbous":
			if lit < 0.5 {
				t.Errorf("%.2f°: %s at %.3f lit", angle, name, lit)
			}
		default:
			t.Fatalf("%.2f°: unexpected phase name %q", angle, name)
		}

		// Waxing before the full moon, waning after it.
		if waning := strings.Contains(name, "Waning"); waning && angle < 180 {
			t.Errorf("%.2f°: %s before the full moon", angle, name)
		}
		if waxing := strings.Contains(name, "Waxing"); waxing && angle > 180 {
			t.Errorf("%.2f°: %s after the full moon", angle, name)
		}
	}
}

// Every eighth of the lunation cycle has to be reachable, and each must begin
// at the aspect it is named for.
func TestLunationPhasesDivideTheCycle(t *testing.T) {
	seen := map[string]bool{}
	for angle := 0.0; angle < 360; angle += 0.25 {
		seen[lunationPhases[int(angle/45)%8]] = true
	}
	for _, name := range lunationPhases {
		if !seen[name] {
			t.Errorf("the %s phase is unreachable", name)
		}
	}
	for i, name := range lunationPhases {
		if got := lunationPhases[int(float64(i)*45/45)%8]; got != name {
			t.Errorf("%.0f° is %s, want %s", float64(i)*45, got, name)
		}
	}
}

// Applying is worked out from the speeds by extrapolation, so the check has to
// come from somewhere else: cast the same birth half an hour later and see
// whether the orb actually closed. Anything that agrees with the ephemeris on
// every pair is right regardless of how it got there.
func TestApplyingAgreesWithTheEphemeris(t *testing.T) {
	c := testChart(t)
	later, err := Compute(Birth{
		Year: 1961, Month: time.August, Day: 4, Hour: 19, Minute: 54,
		Zone: mustZone(t, "Pacific/Honolulu"), ZoneName: "Pacific/Honolulu",
		Latitude: 21.306944, Longitude: -157.858333,
	})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	type pair struct {
		a, b Body
		name string
	}
	orbLater := map[pair]float64{}
	for _, a := range later.Aspects {
		orbLater[pair{a.A, a.B, a.Name}] = a.Orb
	}

	var checked int
	for _, a := range c.Aspects {
		applying, known := c.Applying(a)
		if !known {
			if !a.A.IsAngle() && !a.B.IsAngle() {
				t.Errorf("%s %s %s: applying could not be decided between two bodies",
					a.A, a.Name, a.B)
			}
			continue
		}
		// A pair already within half a degree of exact may pass through it
		// inside the step, which would make the comparison meaningless.
		if a.Orb < 0.5 {
			continue
		}
		after, ok := orbLater[pair{a.A, a.B, a.Name}]
		if !ok {
			continue // the pair drifted out of orb, or into a different aspect
		}
		checked++
		if closing := after < a.Orb; closing != applying {
			t.Errorf("%s %s %s: applying = %v, but the orb went from %.4f° to %.4f°",
				a.A, a.Name, a.B, applying, a.Orb, after)
		}
	}
	if checked == 0 {
		t.Fatal("no aspect between two moving bodies to check")
	}
}

// The angles are house cusps, so the Sun's house says which side of the horizon
// it was on.
func TestSectFollowsTheSun(t *testing.T) {
	c := testChart(t)
	sect, ok := c.Sect()
	if !ok {
		t.Fatal("no sect")
	}
	sun, _ := c.Placement(Sun)
	want := "night"
	if sun.House >= 7 {
		want = "day"
	}
	if sect != want {
		t.Errorf("sect %q with the Sun in the %d house, want %q", sect, sun.House, want)
	}
}
