package astro

import (
	"math"

	"github.com/soniakeys/meeus/v3/nutation"
)

// The chart points that are not read from an ephemeris.
//
// Each of these is arithmetic on quantities the chart already holds — the
// angles, the two luminaries, the sidereal time, the obliquity — so none of
// them needs new astronomical data. What they do need is to say how they were
// arrived at: there is more than one Part of Fortune and more than one Lilith
// in circulation, and a bare longitude with no method beside it cannot be
// compared with anybody else's.
//
// Two points often asked for are missing, and cannot be added here. Chiron and
// the asteroids Ceres, Pallas, Juno and Vesta are minor bodies with no closed
// analytic theory: they need a numerically integrated ephemeris, which this
// application does not carry and could not compute from what it has. They are
// left out rather than approximated.

// Derivation records how a derived point was arrived at, so that a consumer can
// tell whether it is comparing like with like.
type Derivation struct {
	// Method distinguishes the variants of a point that has more than one —
	// "day" or "night" for a lot, "mean" or "true" for a lunar point.
	Method string
	// Formula states the arithmetic in the terms astrologers write it in.
	Formula string
}

// derive computes the derived points and returns them in the order of
// DerivedBodies, skipping any that the chart cannot support.
//
// All of them depend on the angles, so a chart whose Ascendant is unusable has
// no derived points either.
func (c *Chart) derive(jde float64) []Placement {
	sun, okSun := c.byBody[Sun]
	moon, okMoon := c.byBody[Moon]
	if !okSun || !okMoon {
		return nil
	}

	ramc := c.Houses.RAMC
	ε := c.Houses.Obliquity
	φ := c.Houses.Latitude
	asc := c.Houses.Ascendant

	vertex, antiVertex := primeVerticalPoints(ramc, φ, ε)

	longitudes := map[Body]float64{
		PartOfFortune:   partOfFortune(asc, sun.Longitude, moon.Longitude, c.isDay()),
		BlackMoonLilith: meanLilith(jde),
		Vertex:          vertex,
		AntiVertex:      antiVertex,
		EastPoint:       eclipticOfRA((ramc+90)*deg2rad, ε*deg2rad),
	}

	out := make([]Placement, 0, len(DerivedBodies))
	for _, body := range DerivedBodies {
		lon, ok := longitudes[body]
		if !ok {
			continue
		}
		out = append(out, c.placementAt(body, norm360(lon), 0))
	}
	return out
}

// Derivation returns how a derived point was computed, and false for anything
// that came from the ephemeris and needs no explanation.
func (c *Chart) Derivation(b Body) (Derivation, bool) {
	switch b {
	case PartOfFortune:
		if c.isDay() {
			return Derivation{
				Method:  "day",
				Formula: "Ascendant + Moon − Sun",
			}, true
		}
		return Derivation{
			Method:  "night",
			Formula: "Ascendant + Sun − Moon",
		}, true
	case BlackMoonLilith:
		return Derivation{
			Method:  "mean",
			Formula: "mean lunar apogee: mean longitude − mean anomaly + 180°",
		}, true
	case Vertex:
		return Derivation{
			Method:  "western",
			Formula: "the ecliptic degree on the prime vertical west of the meridian",
		}, true
	case AntiVertex:
		return Derivation{
			Method:  "eastern",
			Formula: "the point opposite the Vertex",
		}, true
	case EastPoint:
		return Derivation{
			Method:  "equatorial",
			Formula: "the ecliptic degree whose right ascension is the sidereal time + 90°",
		}, true
	}
	return Derivation{}, false
}

// partOfFortune is the classical lot: the Ascendant carried by the same arc
// that separates the Moon from the Sun, reversed at night so that the lot keeps
// the same relation to the sect light.
func partOfFortune(asc, sun, moon float64, day bool) float64 {
	if day {
		return norm360(asc + moon - sun)
	}
	return norm360(asc + sun - moon)
}

// meanLilith returns the mean lunar apogee, the point of the Moon's orbit
// furthest from Earth, referred to the true equinox of date.
//
// The apogee is the perigee turned around, and the perigee is where the Moon's
// mean longitude and its mean anomaly agree: subtracting one from the other
// leaves the longitude of perigee. Both series are Meeus 47.1 and 47.4.
func meanLilith(jde float64) float64 {
	t := (jde - j2000) / 36525

	// Mean longitude of the Moon.
	lʹ := horner(t, 218.3164477, 481267.88123421, -0.0015786, 1.0/538841, -1.0/65194000)
	// Mean anomaly of the Moon.
	mʹ := horner(t, 134.9633964, 477198.8675055, 0.0087414, 1.0/69699, -1.0/14712000)

	perigee := lʹ - mʹ
	return norm360(perigee + 180 + nutationInLongitude(jde))
}

// primeVerticalPoints returns the two degrees of the ecliptic that lie on the
// prime vertical — the great circle through east, zenith and west — with the
// western one first. Angles are in degrees.
//
// The prime vertical's pole is the north point of the horizon, which sits on
// the meridian at declination 90° − φ. Requiring an ecliptic point to be a
// right angle from that pole reduces to a single arctangent. Doing it this way
// rather than through the usual "Ascendant of the co-latitude" shortcut avoids
// the shortcut's singularity at the equator, where the prime vertical coincides
// with the celestial equator and the answer is exactly 0° Aries and 0° Libra.
func primeVerticalPoints(ramc, φ, ε float64) (vertex, antiVertex float64) {
	sφ, cφ := math.Sincos(φ * deg2rad)
	sr, cr := math.Sincos(ramc * deg2rad)
	sε, cε := math.Sincos(ε * deg2rad)

	// The pole of the prime vertical, rotated into ecliptic coordinates; a
	// point on the ecliptic is on the circle when its dot product with the pole
	// vanishes, which is this arctangent.
	first := norm360(math.Atan2(sφ*cr, cφ*sε-sφ*sr*cε) * rad2deg)
	second := norm360(first + 180)

	// Of the two, the Vertex is the one west of the meridian. A point is west
	// of the meridian exactly when its hour angle lies in the first half turn,
	// which needs no azimuth convention to state.
	if westOfMeridian(first, ramc, ε) {
		return first, second
	}
	return second, first
}

// nutationInLongitude returns Δψ in degrees, which carries a mean position to
// the true equinox of date.
func nutationInLongitude(jde float64) float64 {
	Δψ, _ := nutation.Nutation(jde)
	return Δψ.Deg()
}

// westOfMeridian reports whether the ecliptic degree lon has already crossed
// the meridian, given the sidereal time and the obliquity, all in degrees.
func westOfMeridian(lon, ramc, ε float64) bool {
	s, c := math.Sincos(lon * deg2rad)
	α := math.Atan2(s*math.Cos(ε*deg2rad), c) * rad2deg
	return norm360(ramc-α) < 180
}
