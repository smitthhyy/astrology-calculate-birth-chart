package astro

import (
	"math"

	"github.com/soniakeys/meeus/v3/nutation"
	"github.com/soniakeys/meeus/v3/sidereal"
)

// HouseSystem names the division of the sky used for the twelve houses.
type HouseSystem string

const (
	// Placidus divides each quadrant by time rather than by space, and is
	// the default in nearly all modern natal astrology.
	Placidus HouseSystem = "Placidus"
	// WholeSign gives each house one entire sign, starting from the sign of
	// the Ascendant. It is the fallback where Placidus has no solution.
	WholeSign HouseSystem = "Whole Sign"
)

// Houses holds a computed house division.
type Houses struct {
	System HouseSystem
	// Cusps[0] is the first house cusp (the Ascendant); Cusps[9] is the
	// tenth (the Midheaven, except under Whole Sign). Degrees, [0, 360).
	Cusps [12]float64
	// Ascendant and Midheaven are always the true angles, even when the
	// cusps come from Whole Sign.
	Ascendant float64
	Midheaven float64
	// RAMC is the right ascension of the meridian in degrees, which is also
	// the local apparent sidereal time. It is the one number another program
	// needs to recompute these cusps under a different house system, so it is
	// carried through rather than left inside the calculation.
	RAMC float64
	// Obliquity is the true obliquity of the ecliptic on the day, in degrees.
	// Everything that converts between ecliptic and equatorial coordinates
	// needs it, so it is kept rather than recomputed.
	Obliquity float64
	// Latitude is the observer's geographic latitude in degrees, which the
	// prime-vertical points need as well as the cusps.
	Latitude float64
	// Note explains any fallback that was applied, and is empty otherwise.
	Note string
}

// Descendant returns the point opposite the Ascendant.
func (h Houses) Descendant() float64 { return norm360(h.Ascendant + 180) }

// ImumCoeli returns the point opposite the Midheaven.
func (h Houses) ImumCoeli() float64 { return norm360(h.Midheaven + 180) }

// HouseOf returns the 1-based house containing ecliptic longitude lon.
func (h Houses) HouseOf(lon float64) int {
	lon = norm360(lon)
	for i := 0; i < 12; i++ {
		start := h.Cusps[i]
		span := norm360(h.Cusps[(i+1)%12] - start)
		if span == 0 {
			span = 360 // degenerate, but never leave the point homeless
		}
		if norm360(lon-start) < span {
			return i + 1
		}
	}
	return 12
}

// polarCircle is the latitude beyond which parts of the ecliptic never rise or
// set, so a Placidus cusp can have no solution.
const polarCircle = 66.0

// ComputeHouses returns the house division for an observer at geographic
// latitude lat and longitude lon (degrees, north and east positive) at Julian
// Day jdUT in Universal Time.
func ComputeHouses(system HouseSystem, jdUT, lat, lon float64) Houses {
	jde := jdeFromUT(jdUT)
	_, Δε := nutation.Nutation(jde)
	ε := (nutation.MeanObliquity(jde) + Δε).Rad()

	// Right ascension of the meridian: Greenwich apparent sidereal time,
	// expressed in degrees, advanced by the observer's longitude.
	ramc := norm360(sidereal.Apparent(jdUT).Sec()/240 + lon)

	mc := eclipticOfRA(ramc*deg2rad, ε)
	asc := ascendant(ramc*deg2rad, lat*deg2rad, ε)

	h := Houses{
		Ascendant: asc, Midheaven: mc, RAMC: ramc,
		Obliquity: ε * rad2deg, Latitude: lat,
	}

	if system == Placidus {
		if cusps, ok := placidusCusps(ramc, lat, ε, asc, mc); ok {
			h.System = Placidus
			h.Cusps = cusps
			return h
		}
		h.Note = "Placidus houses have no solution at this latitude, so Whole Sign houses are shown instead."
	}

	h.System = WholeSign
	h.Cusps = wholeSignCusps(asc)
	return h
}

// eclipticOfRA returns, in degrees, the ecliptic longitude of the point on the
// ecliptic whose right ascension is ra (radians).
func eclipticOfRA(ra, ε float64) float64 {
	s, c := math.Sincos(ra)
	return norm360(math.Atan2(s, c*math.Cos(ε)) * rad2deg)
}

// ascendant returns the rising degree of the ecliptic in degrees, for a
// meridian at right ascension ramc and geographic latitude φ (both radians).
func ascendant(ramc, φ, ε float64) float64 {
	s, c := math.Sincos(ramc)
	sε, cε := math.Sincos(ε)
	return norm360(math.Atan2(c, -(s*cε+math.Tan(φ)*sε)) * rad2deg)
}

// placidusCusps solves for the four intermediate cusps of the eastern half of
// the chart and mirrors them into the west. It reports false when a cusp has
// no solution, which happens inside the polar circles where the relevant
// degree of the ecliptic never crosses the horizon.
func placidusCusps(ramc, lat, ε, asc, mc float64) ([12]float64, bool) {
	var cusps [12]float64
	if math.Abs(lat) >= polarCircle {
		return cusps, false
	}

	// Each intermediate cusp sits a fixed fraction of the way through a
	// semi-arc. The eastern semi-arcs run MC → Ascendant above the horizon
	// (diurnal, 90° + AD) and Ascendant → IC below it (nocturnal, 90° − AD),
	// where AD is the ascensional difference of the cusp itself — hence the
	// iteration.
	solve := func(guessOffset float64, next func(ad float64) float64) (float64, bool) {
		ra := ramc + guessOffset
		for i := 0; i < 40; i++ {
			// Declination of the point of the ecliptic at this right
			// ascension: tan δ = tan ε · sin α.
			δ := math.Atan(math.Tan(ε) * math.Sin(ra*deg2rad))
			t := math.Tan(lat*deg2rad) * math.Tan(δ)
			if math.Abs(t) > 1 {
				return 0, false // circumpolar: no solution
			}
			ad := math.Asin(t) * rad2deg
			updated := norm360(ramc + next(ad))
			if math.Abs(arcSeparation(ra, updated)) < 1e-9 {
				ra = updated
				break
			}
			ra = updated
		}
		return eclipticOfRA(ra*deg2rad, ε), true
	}

	type spec struct {
		house  int // 1-based house whose cusp this is
		offset float64
		next   func(ad float64) float64
	}
	specs := []spec{
		{11, 30, func(ad float64) float64 { return (90 + ad) / 3 }},
		{12, 60, func(ad float64) float64 { return 2 * (90 + ad) / 3 }},
		{2, 120, func(ad float64) float64 { return 180 - 2*(90-ad)/3 }},
		{3, 150, func(ad float64) float64 { return 180 - (90-ad)/3 }},
	}
	for _, s := range specs {
		c, ok := solve(s.offset, s.next)
		if !ok {
			return cusps, false
		}
		cusps[s.house-1] = c
		cusps[(s.house+5)%12] = norm360(c + 180)
	}

	cusps[0] = asc
	cusps[6] = norm360(asc + 180)
	cusps[9] = mc
	cusps[3] = norm360(mc + 180)

	if !cuspsAreOrdered(cusps) {
		return cusps, false
	}
	return cusps, true
}

// cuspsAreOrdered checks that the cusps run once around the zodiac in order.
// A converged-but-wrong Placidus solution shows up as a cusp out of sequence,
// and it is better to fall back than to draw a tangled chart.
func cuspsAreOrdered(cusps [12]float64) bool {
	total := 0.0
	for i := 0; i < 12; i++ {
		span := norm360(cusps[(i+1)%12] - cusps[i])
		if span <= 0 || span >= 180 {
			return false
		}
		total += span
	}
	return math.Abs(total-360) < 1e-6
}

// wholeSignCusps gives each house the whole of one sign, beginning with the
// sign the Ascendant falls in.
func wholeSignCusps(asc float64) [12]float64 {
	var cusps [12]float64
	start := 30 * math.Floor(norm360(asc)/30)
	for i := range cusps {
		cusps[i] = norm360(start + float64(30*i))
	}
	return cusps
}
