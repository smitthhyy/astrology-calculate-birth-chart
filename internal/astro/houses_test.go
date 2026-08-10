package astro

import (
	"math"
	"testing"
)

// The expected cusps below were produced by the Swiss Ephemeris 2.10.03
// (swe_houses_ex with house system 'P') for the same instant and place, and
// serve as the independent reference for this package's Placidus solver.
var placidusCases = []struct {
	name     string
	jdUT     float64
	lat, lon float64
	cusps    [12]float64
}{
	{
		// 1961-08-05 05:24 UT, Honolulu.
		name: "Honolulu, low northern latitude",
		jdUT: 2437516.725, lat: 21.306944, lon: -157.858333,
		cusps: [12]float64{318.051785, 355.908184, 30.298913, 58.893444, 83.980861, 109.022576,
			138.051785, 175.908184, 210.298913, 238.893444, 263.980861, 289.022576},
	},
	{
		// 1879-03-14 10:36:32 UT, Ulm.
		name: "Ulm, mid northern latitude",
		jdUT: 2407422.942037037, lat: 48.398333, lon: 9.991667,
		cusps: [12]float64{98.824958, 115.872809, 134.788240, 159.212749, 194.002179, 239.132617,
			278.824958, 295.872809, 314.788240, 339.212749, 14.002179, 59.132617},
	},
	{
		// 1985-06-21 12:00 UT, London.
		name: "London, high northern latitude",
		jdUT: 2446238.0, lat: 51.5074, lon: -0.1278,
		cusps: [12]float64{179.646775, 203.484265, 233.512881, 269.540554, 305.617092, 335.724623,
			359.646775, 23.484265, 53.512881, 89.540554, 125.617092, 155.724623},
	},
	{
		// 2001-11-03 14:45 UT, Sydney — southern hemisphere.
		name: "Sydney, southern hemisphere",
		jdUT: 2452217.114583333, lat: -33.8688, lon: 151.2093,
		cusps: [12]float64{130.668078, 173.762856, 210.093991, 237.652713, 260.658446, 283.323181,
			310.668078, 353.762856, 30.093991, 57.652713, 80.658446, 103.323181},
	},
	{
		// 1970-01-15 03:20 UT, Reykjavik — just inside the polar circle,
		// where the quadrants become very lopsided.
		name: "Reykjavik, sub-polar",
		jdUT: 2440601.638888889, lat: 64.1466, lon: -21.9426,
		cusps: [12]float64{209.747334, 234.576117, 270.974446, 319.810860, 353.476026, 15.128034,
			29.747334, 54.576117, 90.974446, 139.810860, 173.476026, 195.128034},
	},
	{
		// 1995-09-09 21:05 UT, Nairobi — almost on the equator.
		name: "Nairobi, equatorial",
		jdUT: 2449970.378472222, lat: -1.2921, lon: 36.8219,
		cusps: [12]float64{72.452375, 100.247410, 128.942128, 159.978023, 192.477030, 223.723573,
			252.452375, 280.247410, 308.942128, 339.978023, 12.477030, 43.723573},
	},
}

func TestPlacidusCuspsMatchSwissEphemeris(t *testing.T) {
	// 5 arcseconds. The residual is dominated by the two libraries' slightly
	// different nutation and ΔT models, not by the cusp solver.
	const tol = 5 * arcsec

	for _, tc := range placidusCases {
		t.Run(tc.name, func(t *testing.T) {
			h := ComputeHouses(Placidus, tc.jdUT, tc.lat, tc.lon)
			if h.System != Placidus {
				t.Fatalf("fell back to %s: %s", h.System, h.Note)
			}
			for i, want := range tc.cusps {
				got := h.Cusps[i]
				if d := math.Abs(arcSeparation(want, got)); d > tol {
					t.Errorf("cusp %d = %.6f°, want %.6f° (off by %.2f\")", i+1, got, want, d/arcsec)
				}
			}
			if d := math.Abs(arcSeparation(tc.cusps[0], h.Ascendant)); d > tol {
				t.Errorf("ascendant = %.6f°, want %.6f°", h.Ascendant, tc.cusps[0])
			}
			if d := math.Abs(arcSeparation(tc.cusps[9], h.Midheaven)); d > tol {
				t.Errorf("midheaven = %.6f°, want %.6f°", h.Midheaven, tc.cusps[9])
			}
		})
	}
}

func TestPlacidusFallsBackInsidePolarCircle(t *testing.T) {
	// Tromsø, 69.6°N. Swiss Ephemeris itself refuses Placidus here.
	h := ComputeHouses(Placidus, julianDay(1980, 12, 1+8.0/24), 69.6492, 18.9553)
	if h.System != WholeSign {
		t.Fatalf("system = %s, want %s", h.System, WholeSign)
	}
	if h.Note == "" {
		t.Error("expected a note explaining the fallback")
	}
	if got, want := h.Cusps[0], 30*math.Floor(h.Ascendant/30); math.Abs(got-want) > 1e-9 {
		t.Errorf("first whole-sign cusp = %.6f, want %.6f", got, want)
	}
	for i := 1; i < 12; i++ {
		if d := math.Abs(arcSeparation(h.Cusps[i-1]+30, h.Cusps[i])); d > 1e-9 {
			t.Errorf("whole-sign cusp %d is not 30° after cusp %d", i+1, i)
		}
	}
}

func TestHouseOf(t *testing.T) {
	h := ComputeHouses(Placidus, 2437516.725, 21.306944, -157.858333)
	tests := []struct {
		lon  float64
		want int
	}{
		{h.Cusps[0], 1},
		{h.Cusps[0] + 0.001, 1},
		{h.Cusps[1] - 0.001, 1},
		{h.Cusps[1], 2},
		{h.Cusps[9], 10},
		{h.Cusps[11], 12},
		{h.Cusps[0] - 0.001, 12},
		{132.547929, 6},  // Obama's Sun, 12°32' Leo
		{300.858602, 12}, // Obama's Jupiter, 0°51' Aquarius
	}
	for _, tc := range tests {
		if got := h.HouseOf(tc.lon); got != tc.want {
			t.Errorf("HouseOf(%.6f) = %d, want %d", tc.lon, got, tc.want)
		}
	}
}

func TestAnglesAreOpposed(t *testing.T) {
	h := ComputeHouses(Placidus, 2446238.0, 51.5074, -0.1278)
	if d := math.Abs(arcSeparation(h.Ascendant+180, h.Descendant())); d > 1e-9 {
		t.Errorf("descendant is %.9f° from opposing the ascendant", d)
	}
	if d := math.Abs(arcSeparation(h.Midheaven+180, h.ImumCoeli())); d > 1e-9 {
		t.Errorf("IC is %.9f° from opposing the midheaven", d)
	}
}
