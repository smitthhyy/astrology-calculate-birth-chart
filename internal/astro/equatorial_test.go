package astro

import (
	"math"
	"testing"
	"time"
)

// The equatorial coordinates have to agree with the ecliptic ones they came
// from, which is a closed identity: sin δ = sin β cos ε + cos β sin ε sin λ.
// Checking every placement against it catches a swapped argument or a degrees-
// for-radians slip anywhere in the chart.
func TestEquatorialCoordinatesAgreeWithTheEcliptic(t *testing.T) {
	chart := goldenChart(t)
	ε := chart.Houses.Obliquity * deg2rad

	all := append(append([]Placement{}, chart.Placements...), chart.Derived...)
	for _, p := range all {
		sλ, cλ := math.Sincos(p.Longitude * deg2rad)
		sβ, cβ := math.Sincos(p.Latitude * deg2rad)
		sε, cε := math.Sincos(ε)

		wantδ := math.Asin(sβ*cε+cβ*sε*sλ) * rad2deg
		wantα := norm360(math.Atan2(sλ*cε-(sβ/cβ)*sε, cλ) * rad2deg)

		if math.Abs(p.Declination-wantδ) > 1e-9 {
			t.Errorf("%s: declination %.9f, want %.9f", p.Body.Name(), p.Declination, wantδ)
		}
		if d := math.Abs(arcSeparation(p.RightAscension, wantα)); d > 1e-9 {
			t.Errorf("%s: right ascension %.9f, want %.9f", p.Body.Name(), p.RightAscension, wantα)
		}
		if p.RightAscension < 0 || p.RightAscension >= 360 {
			t.Errorf("%s: right ascension %v is out of range", p.Body.Name(), p.RightAscension)
		}
		if p.Declination < -90 || p.Declination > 90 {
			t.Errorf("%s: declination %v is out of range", p.Body.Name(), p.Declination)
		}
	}
}

// The four places where the two coordinate systems have known answers: the
// equinoxes coincide, and each solstice sits at the obliquity.
func TestEquatorialCoordinatesAtTheCardinalPoints(t *testing.T) {
	chart := goldenChart(t)
	ε := chart.Houses.Obliquity

	cases := []struct {
		lon, wantRA, wantDec float64
	}{
		{0, 0, 0},     // the vernal point
		{90, 90, ε},   // the northern solstice
		{180, 180, 0}, // the autumnal point
		{270, 270, -ε},
	}
	for _, tc := range cases {
		p := chart.placementAt(Sun, tc.lon, 0)
		if d := math.Abs(arcSeparation(p.RightAscension, tc.wantRA)); d > 1e-9 {
			t.Errorf("longitude %.0f: right ascension %.6f, want %.6f", tc.lon, p.RightAscension, tc.wantRA)
		}
		if math.Abs(p.Declination-tc.wantDec) > 1e-9 {
			t.Errorf("longitude %.0f: declination %.6f, want %.6f", tc.lon, p.Declination, tc.wantDec)
		}
	}
}

// The Sun's declination is the one figure here anybody can check against a
// calendar: at the June solstice it stands at the obliquity, and at the equinoxes
// it crosses zero.
func TestSunDeclinationThroughTheYear(t *testing.T) {
	cases := []struct {
		name    string
		month   time.Month
		day     int
		wantDec float64
		within  float64
	}{
		{"March equinox", time.March, 20, 0, 0.5},
		{"June solstice", time.June, 21, 23.44, 0.1},
		{"September equinox", time.September, 23, 0, 0.5},
		{"December solstice", time.December, 21, -23.44, 0.1},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			chart, err := Compute(Birth{
				Year: 2000, Month: tc.month, Day: tc.day, Hour: 12,
				Zone: time.UTC, ZoneName: "UTC",
				Latitude: 51.5, Longitude: 0,
			})
			if err != nil {
				t.Fatalf("Compute: %v", err)
			}
			sun, _ := chart.Placement(Sun)
			if math.Abs(sun.Declination-tc.wantDec) > tc.within {
				t.Errorf("declination = %.4f, want %.2f ± %.2f", sun.Declination, tc.wantDec, tc.within)
			}
		})
	}
}

// The Midheaven is on the meridian by definition, so its right ascension is the
// sidereal time the houses were built from. If the two disagree the equatorial
// conversion and the house division are using different obliquities.
func TestMidheavenRightAscensionIsTheSiderealTime(t *testing.T) {
	chart := goldenChart(t)
	mc, _ := chart.Placement(Midheaven)
	if d := math.Abs(arcSeparation(mc.RightAscension, chart.Houses.RAMC)); d > 1e-9 {
		t.Errorf("Midheaven right ascension %.9f, sidereal time %.9f", mc.RightAscension, chart.Houses.RAMC)
	}
}
