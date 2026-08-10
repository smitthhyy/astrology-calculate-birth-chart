package astro

import (
	"math"
	"testing"
)

// Apparent geocentric ecliptic longitudes from Swiss Ephemeris 2.10.03
// (swe_calc_ut with the built-in Moshier ephemeris), spanning the range of
// birth dates the application has to serve.
var swissLongitudes = []struct {
	name string
	jdUT float64
	lon  map[Body]float64
}{
	{
		name: "1905-06-30 12:00 UT", jdUT: 2417027.000000000,
		lon: map[Body]float64{
			Sun: 97.993065, Moon: 71.149021, Mercury: 105.278968, Venus: 52.498135,
			Mars: 219.447889, Jupiter: 56.089710, Saturn: 332.666167, Uranus: 272.004667,
			Neptune: 97.889314, Pluto: 81.609529,
		},
	},
	{
		name: "1961-08-05 05:24 UT", jdUT: 2437516.725000000,
		lon: map[Body]float64{
			Sun: 132.547929, Moon: 63.357615, Mercury: 122.331621, Venus: 91.789377,
			Mars: 172.576603, Jupiter: 300.858602, Saturn: 295.330775, Uranus: 145.270768,
			Neptune: 218.605884, Pluto: 156.977946,
		},
	},
	{
		name: "1990-06-15 04:30 UT", jdUT: 2448057.687500000,
		lon: map[Body]float64{
			Sun: 83.831092, Moon: 341.199610, Mercury: 65.156714, Venus: 48.410161,
			Mars: 10.816588, Jupiter: 105.821765, Saturn: 294.050237, Uranus: 278.177393,
			Neptune: 283.724908, Pluto: 225.407875,
		},
	},
	{
		name: "2024-02-29 18:07 UT", jdUT: 2460370.254861111,
		lon: map[Body]float64{
			Sun: 340.643448, Moon: 217.510108, Mercury: 341.861076, Venus: 316.189586,
			Mars: 312.727337, Jupiter: 41.272458, Saturn: 339.883261, Uranus: 49.572273,
			Neptune: 356.731485, Pluto: 301.209521,
		},
	},
}

func TestLongitudesMatchSwissEphemeris(t *testing.T) {
	// Meeus's Pluto series (chapter 37) is a coarser theory than the rest, so
	// it gets a looser bound; everything else has to agree to a few arcseconds.
	tolerance := func(b Body) float64 {
		switch b {
		case Pluto:
			return 60 * arcsec
		case Moon:
			return 20 * arcsec
		default:
			return 5 * arcsec
		}
	}

	for _, tc := range swissLongitudes {
		t.Run(tc.name, func(t *testing.T) {
			jde := jdeFromUT(tc.jdUT)
			for _, b := range []Body{Sun, Moon, Mercury, Venus, Mars, Jupiter, Saturn, Uranus, Neptune, Pluto} {
				got, _, _, err := apparentPosition(b, jde)
				if err != nil {
					t.Fatalf("%s: %v", b, err)
				}
				want := tc.lon[b]
				if d := math.Abs(arcSeparation(want, got)); d > tolerance(b) {
					t.Errorf("%-8s %.6f°, want %.6f° (off by %.1f\")", b, got, want, d/arcsec)
				}
			}
		})
	}
}
