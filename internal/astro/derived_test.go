package astro

import (
	"math"
	"testing"
	"time"
)

// TestPrimeVerticalPointsLieOnThePrimeVertical checks the Vertex against its own
// definition rather than against a remembered number.
//
// A point is on the prime vertical when its altitude and its azimuth put it due
// east or due west, which reduces to a single condition: the component of the
// direction along the horizon's north-south axis vanishes, cos φ · sin δ −
// sin φ · cos δ · cos H = 0. That is what the Vertex has to satisfy, whatever
// formula produced it.
func TestPrimeVerticalPointsLieOnThePrimeVertical(t *testing.T) {
	cases := []struct {
		name       string
		ramc, φ, ε float64
	}{
		{"northern mid-latitude", 120, 51.5, 23.44},
		{"southern mid-latitude", 300, -33.9, 23.44},
		{"the tropics", 45, 21.3, 23.44},
		{"just inside the arctic circle", 200, 64.1, 23.44},
		{"sidereal time at the equinox point", 0, 40, 23.44},
		{"sidereal time at the solstice point", 90, 40, 23.44},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vertex, anti := primeVerticalPoints(tc.ramc, tc.φ, tc.ε)

			for _, lon := range []float64{vertex, anti} {
				if got := onPrimeVertical(lon, tc.ramc, tc.φ, tc.ε); math.Abs(got) > 1e-9 {
					t.Errorf("%.4f° is %g off the prime vertical", lon, got)
				}
			}
			if sep := math.Abs(arcSeparation(vertex, anti)); math.Abs(sep-180) > 1e-9 {
				t.Errorf("Vertex and Anti-Vertex are %.6f° apart, want 180", sep)
			}
			if !westOfMeridian(vertex, tc.ramc, tc.ε) {
				t.Errorf("the Vertex at %.4f° is east of the meridian", vertex)
			}
			if westOfMeridian(anti, tc.ramc, tc.ε) {
				t.Errorf("the Anti-Vertex at %.4f° is west of the meridian", anti)
			}
		})
	}
}

// onPrimeVertical returns how far the ecliptic degree lon is from the prime
// vertical, as the residual of the defining condition. Zero means it is on it.
func onPrimeVertical(lon, ramc, φ, ε float64) float64 {
	sλ, cλ := math.Sincos(lon * deg2rad)
	sε, cε := math.Sincos(ε * deg2rad)

	α := math.Atan2(sλ*cε, cλ)
	δ := math.Asin(sλ * sε)
	h := norm360(ramc-α*rad2deg) * deg2rad

	sφ, cφ := math.Sincos(φ * deg2rad)
	sδ, cδ := math.Sincos(δ)
	return cφ*sδ - sφ*cδ*math.Cos(h)
}

// At the equator the prime vertical runs through the poles and the celestial
// equator crosses the ecliptic at the equinoxes, so the two points are exactly
// the first degree of Aries and of Libra whatever the sidereal time. This is the
// case the "Ascendant of the co-latitude" shortcut cannot state.
func TestPrimeVerticalAtTheEquator(t *testing.T) {
	for _, ramc := range []float64{0, 37, 123.5, 270} {
		vertex, anti := primeVerticalPoints(ramc, 0, 23.44)
		for _, lon := range []float64{vertex, anti} {
			if d := math.Abs(arcSeparation(lon, 0)); d > 1e-9 && math.Abs(d-180) > 1e-9 {
				t.Errorf("ramc %.1f: got %.6f°, want 0° Aries or 0° Libra", ramc, lon)
			}
		}
	}
}

// The Part of Fortune keeps the same distance from the Ascendant that the sect
// light keeps from the other luminary, and the day and night versions are
// reflections of each other in the Ascendant.
func TestPartOfFortune(t *testing.T) {
	const asc, sun, moon = 100, 20, 350

	day := partOfFortune(asc, sun, moon, true)
	night := partOfFortune(asc, sun, moon, false)

	if want := norm360(asc + moon - sun); day != want {
		t.Errorf("day lot = %.4f, want %.4f", day, want)
	}
	if got, want := arcSeparation(asc, day), -arcSeparation(asc, night); math.Abs(got-want) > 1e-9 {
		t.Errorf("the two lots are not reflections in the Ascendant: %.4f vs %.4f", got, want)
	}
	// The lot is where the Moon would be if the Sun were rising.
	if got, want := arcSeparation(asc, day), arcSeparation(sun, moon); math.Abs(got-want) > 1e-9 {
		t.Errorf("Ascendant-to-lot is %.4f°, Sun-to-Moon is %.4f°", got, want)
	}
}

// Black Moon Lilith is the mean lunar apogee, which goes once round the zodiac
// in 8.85 years. That period is the check on the series: the individual terms
// are Meeus's, but a sign error or a transposed coefficient would show up as the
// wrong rate.
func TestMeanLilithCycleLength(t *testing.T) {
	const jd = 2451545.0
	const years = 8.850578 // the known period of the apsidal line

	start := meanLilith(jd)
	after := meanLilith(jd + years*365.25)
	if d := math.Abs(arcSeparation(start, after)); d > 0.2 {
		t.Errorf("after one apsidal cycle Lilith moved %.4f°, want back where it started", d)
	}

	// And it moves forward, not backward: the apsides advance.
	if step := arcSeparation(start, meanLilith(jd+30)); step <= 0 {
		t.Errorf("Lilith moved %.4f° in a month, want a forward motion", step)
	}
}

// TestDerivedPointsAreComputed checks that a chart carries the whole set, that
// each one says how it was arrived at, and that none of them was silently left
// at zero.
func TestDerivedPointsAreComputed(t *testing.T) {
	chart := goldenChart(t)

	if len(chart.Derived) != len(DerivedBodies) {
		t.Fatalf("got %d derived points, want %d", len(chart.Derived), len(DerivedBodies))
	}
	for i, p := range chart.Derived {
		if p.Body != DerivedBodies[i] {
			t.Errorf("derived[%d] = %s, want %s", i, p.Body.Name(), DerivedBodies[i].Name())
		}
		if p.Longitude < 0 || p.Longitude >= 360 {
			t.Errorf("%s: longitude %v is out of range", p.Body.Name(), p.Longitude)
		}
		if p.House < 1 || p.House > 12 {
			t.Errorf("%s: house %d is out of range", p.Body.Name(), p.House)
		}
		if _, ok := chart.Derivation(p.Body); !ok {
			t.Errorf("%s has no derivation", p.Body.Name())
		}
		if _, ok := chart.Placement(p.Body); !ok {
			t.Errorf("%s is not reachable through Placement", p.Body.Name())
		}
	}

	// The Anti-Vertex is opposite the Vertex, which is the one relationship
	// between two derived points that is fixed by definition.
	vx, _ := chart.Placement(Vertex)
	avx, _ := chart.Placement(AntiVertex)
	if sep := math.Abs(arcSeparation(vx.Longitude, avx.Longitude)); math.Abs(sep-180) > 1e-9 {
		t.Errorf("Vertex and Anti-Vertex are %.6f° apart, want 180", sep)
	}
}

// The derived points must not become house occupants or table rows: the pages
// and the readings are built from Placements, and nothing there has text written
// for it.
func TestDerivedPointsStayOutOfTheTables(t *testing.T) {
	chart := goldenChart(t)

	for _, p := range chart.Placements {
		if p.Body.IsDerived() {
			t.Errorf("%s appears in Placements", p.Body.Name())
		}
	}
	for _, h := range chart.Houses12 {
		for _, b := range h.Occupants {
			if b.IsDerived() {
				t.Errorf("%s is listed as an occupant of house %d", b.Name(), h.Number)
			}
		}
	}
	for _, a := range chart.Aspects {
		if a.A.IsDerived() || a.B.IsDerived() {
			t.Errorf("a derived point formed an aspect: %s %s %s", a.A.Name(), a.Name, a.B.Name())
		}
	}
}

// The Part of Fortune follows the sect, so the same birthplace at noon and at
// midnight must give the two different lots.
func TestPartOfFortuneFollowsTheSect(t *testing.T) {
	base := Birth{
		Year: 1990, Month: time.June, Day: 15,
		Zone: time.UTC, ZoneName: "UTC",
		Latitude: 51.5, Longitude: 0,
	}

	lot := func(hour int) (float64, string) {
		b := base
		b.Hour = hour
		chart, err := Compute(b)
		if err != nil {
			t.Fatalf("Compute: %v", err)
		}
		p, ok := chart.Placement(PartOfFortune)
		if !ok {
			t.Fatal("no Part of Fortune")
		}
		d, _ := chart.Derivation(PartOfFortune)
		return p.Longitude, d.Method
	}

	_, dayMethod := lot(12)
	_, nightMethod := lot(0)
	if dayMethod != "day" {
		t.Errorf("a noon chart is a %s chart, want day", dayMethod)
	}
	if nightMethod != "night" {
		t.Errorf("a midnight chart is a %s chart, want night", nightMethod)
	}
}

func goldenChart(t *testing.T) *Chart {
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
