package astro

import (
	"math"
	"testing"
)

// arcsec expresses a tolerance in degrees.
const arcsec = 1.0 / 3600

func TestApparentPositionMeeusExamples(t *testing.T) {
	tests := []struct {
		name    string
		body    Body
		jde     float64
		lon     float64 // apparent geocentric ecliptic longitude, degrees
		lat     float64 // apparent geocentric ecliptic latitude, degrees
		dist    float64 // AU; zero means "don't check"
		tolArc  float64 // tolerance in arcseconds for lon and lat
		tolDist float64
	}{
		{
			// Meeus, Astronomical Algorithms 2nd ed., example 25.b:
			// the Sun on 1992 October 13.0 TD. Apparent λ = 199°54'21.56".
			name:   "Sun 1992-10-13",
			body:   Sun,
			jde:    2448908.5,
			lon:    199 + 54.0/60 + 21.56/3600,
			lat:    0.00019, // +0.72" of heliocentric latitude, reflected
			tolArc: 1.5,
		},
		{
			// Example 33.a: Venus on 1992 December 20.0 TD.
			name:    "Venus 1992-12-20",
			body:    Venus,
			jde:     2448976.5,
			lon:     313.08102,
			lat:     -2.08474,
			dist:    0.910947,
			tolArc:  2,
			tolDist: 1e-6,
		},
		{
			// Example 47.a: the Moon on 1992 April 12.0 TD. The book quotes
			// the position for the mean equinox without nutation, so the
			// apparent longitude here is larger by Δψ = +16.595".
			name:    "Moon 1992-04-12",
			body:    Moon,
			jde:     2448724.5,
			lon:     133.162655 + 16.595/3600,
			lat:     -3.229126,
			dist:    368409.7 / kmPerAU,
			tolArc:  2,
			tolDist: 1e-6,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lon, lat, dist, err := apparentPosition(tc.body, tc.jde)
			if err != nil {
				t.Fatalf("apparentPosition: %v", err)
			}
			if d := math.Abs(arcSeparation(tc.lon, lon)); d > tc.tolArc*arcsec {
				t.Errorf("longitude %.6f°, want %.6f° (off by %.2f\")", lon, tc.lon, d/arcsec)
			}
			if d := math.Abs(lat - tc.lat); d > tc.tolArc*arcsec {
				t.Errorf("latitude %.6f°, want %.6f° (off by %.2f\")", lat, tc.lat, d/arcsec)
			}
			if tc.dist != 0 && math.Abs(dist-tc.dist) > tc.tolDist {
				t.Errorf("distance %.9f AU, want %.9f AU", dist, tc.dist)
			}
		})
	}
}

func TestLongitudeSpeedSignsRetrograde(t *testing.T) {
	// Mercury was retrograde from 2024-08-05 to 2024-08-28, and direct on
	// either side of that window.
	tests := []struct {
		name       string
		jde        float64
		retrograde bool
	}{
		{"direct before station", julianDay(2024, 7, 20.5), false},
		{"mid retrograde", julianDay(2024, 8, 15.5), true},
		{"direct after station", julianDay(2024, 9, 15.5), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			speed, err := longitudeSpeed(Mercury, tc.jde)
			if err != nil {
				t.Fatalf("longitudeSpeed: %v", err)
			}
			if got := speed < 0; got != tc.retrograde {
				t.Errorf("speed %.4f°/day, retrograde=%v, want retrograde=%v", speed, got, tc.retrograde)
			}
		})
	}
}

func TestSouthNodeOpposesNorthNode(t *testing.T) {
	jde := julianDay(1990, 6, 15.5)
	north, _, _, err := apparentPosition(NorthNode, jde)
	if err != nil {
		t.Fatalf("north node: %v", err)
	}
	south, _, _, err := apparentPosition(SouthNode, jde)
	if err != nil {
		t.Fatalf("south node: %v", err)
	}
	if d := math.Abs(arcSeparation(north+180, south)); d > 1e-9 {
		t.Errorf("nodes are %.9f° from opposition", d)
	}
}
