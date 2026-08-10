package astro

import (
	"fmt"
	"math"

	"github.com/soniakeys/meeus/v3/base"
	"github.com/soniakeys/meeus/v3/coord"
	"github.com/soniakeys/meeus/v3/moonposition"
	"github.com/soniakeys/meeus/v3/nutation"
	"github.com/soniakeys/meeus/v3/pluto"
	"github.com/soniakeys/meeus/v3/precess"
	"github.com/soniakeys/unit"
)

// Body is a point plotted on the chart: a planet, a luminary, a lunar node or
// one of the four angles.
type Body int

// The bodies, in the order they are presented.
const (
	Sun Body = iota
	Moon
	Mercury
	Venus
	Mars
	Jupiter
	Saturn
	Uranus
	Neptune
	Pluto
	Ascendant
	Descendant
	Midheaven
	ImumCoeli
	NorthNode
	SouthNode
	// The points below are derived from the angles, the luminaries and the
	// lunar orbit rather than read from an ephemeris. They are not part of the
	// reading, so they appear only in the export.
	PartOfFortune
	BlackMoonLilith
	Vertex
	AntiVertex
	EastPoint
	numBodies
)

// TableBodies are the chart points that appear in the main interpretation
// table: every planet plus the Ascendant and Descendant.
var TableBodies = []Body{
	Sun, Moon, Mercury, Venus, Mars, Jupiter, Saturn, Uranus, Neptune, Pluto,
	Ascendant, Descendant,
}

// PointBodies are the secondary angles and points, shown with their placement
// but without a per-sign reading.
var PointBodies = []Body{Midheaven, ImumCoeli, NorthNode, SouthNode}

// DerivedBodies are the further points a chart can carry, each computed from
// quantities the chart already holds rather than from an ephemeris.
//
// They are kept apart from PointBodies because there is no written reading for
// them and they are not house occupants: they belong in the exported data, for
// a consumer that reads lots and prime-vertical points, and nowhere else.
var DerivedBodies = []Body{PartOfFortune, BlackMoonLilith, Vertex, AntiVertex, EastPoint}

// WheelBodies are the points drawn as glyphs inside the chart wheel. The
// angles are drawn as axes instead, so they are not repeated here.
var WheelBodies = []Body{
	Sun, Moon, Mercury, Venus, Mars, Jupiter, Saturn, Uranus, Neptune, Pluto,
	NorthNode,
}

type bodyMeta struct {
	key   string
	name  string
	glyph string
}

var bodyInfo = [numBodies]bodyMeta{
	Sun:        {"sun", "Sun", "☉"},
	Moon:       {"moon", "Moon", "☽"},
	Mercury:    {"mercury", "Mercury", "☿"},
	Venus:      {"venus", "Venus", "♀"},
	Mars:       {"mars", "Mars", "♂"},
	Jupiter:    {"jupiter", "Jupiter", "♃"},
	Saturn:     {"saturn", "Saturn", "♄"},
	Uranus:     {"uranus", "Uranus", "♅"},
	Neptune:    {"neptune", "Neptune", "♆"},
	Pluto:      {"pluto", "Pluto", "♇"},
	Ascendant:  {"ascendant", "Ascendant", "AC"},
	Descendant: {"descendant", "Descendant", "DC"},
	Midheaven:  {"midheaven", "Midheaven", "MC"},
	ImumCoeli:  {"imumCoeli", "Imum Coeli", "IC"},
	NorthNode:  {"northNode", "North Node", "☊"},
	SouthNode:  {"southNode", "South Node", "☋"},

	PartOfFortune:   {"partOfFortune", "Part of Fortune", "⊗"},
	BlackMoonLilith: {"blackMoonLilith", "Black Moon Lilith", "⚸"},
	Vertex:          {"vertex", "Vertex", "Vx"},
	AntiVertex:      {"antiVertex", "Anti-Vertex", "AVx"},
	EastPoint:       {"eastPoint", "East Point", "EP"},
}

// Key returns the body's identifier as used in the interpretation data.
func (b Body) Key() string { return bodyInfo[b].key }

// Name returns the body's display name.
func (b Body) Name() string { return bodyInfo[b].name }

// Glyph returns the body's astrological symbol.
func (b Body) Glyph() string { return bodyInfo[b].glyph }

func (b Body) String() string { return bodyInfo[b].name }

// BodyByName looks a body up by its display name. Sign.Ruler names a planet in
// prose, so something has to turn that back into a body.
func BodyByName(name string) (Body, bool) {
	for i, meta := range bodyInfo {
		if meta.name == name {
			return Body(i), true
		}
	}
	return 0, false
}

// Category groups the body, for consumers that treat the kinds differently. A
// luminary carries a wider orb; an angle has no rate of motion; a lot is
// arithmetic on other positions rather than a place in space.
func (b Body) Category() string {
	switch {
	case b == Sun || b == Moon:
		return "luminary"
	case b == NorthNode || b == SouthNode:
		return "node"
	case b == PartOfFortune:
		return "lot"
	case b == Vertex || b == AntiVertex || b == EastPoint:
		return "sensitivePoint"
	case b == BlackMoonLilith:
		return "lunarPoint"
	case b.IsAngle():
		return "angle"
	default:
		return "planet"
	}
}

// IsDerived reports whether the body is one of the points computed from the
// chart's own angles and luminaries rather than from an ephemeris.
func (b Body) IsDerived() bool {
	switch b {
	case PartOfFortune, BlackMoonLilith, Vertex, AntiVertex, EastPoint:
		return true
	}
	return false
}

// IsAngle reports whether the body is one of the four chart angles, which are
// derived from the houses rather than from an ephemeris.
func (b Body) IsAngle() bool {
	switch b {
	case Ascendant, Descendant, Midheaven, ImumCoeli:
		return true
	}
	return false
}

// vsopOf maps a body to its VSOP87D table, if it has one.
var vsopOf = map[Body]vsopPlanet{
	Mercury: vMercury,
	Venus:   vVenus,
	Mars:    vMars,
	Jupiter: vJupiter,
	Saturn:  vSaturn,
	Uranus:  vUranus,
	Neptune: vNeptune,
}

// lightTime is the number of days light takes to travel one astronomical unit.
const lightTime = 0.0057755183

// auPerKm converts kilometres to astronomical units.
const kmPerAU = 149597870.7

// apparentPosition returns the apparent geocentric ecliptic longitude and
// latitude of b in degrees, referred to the true equinox and ecliptic of date,
// together with its distance from Earth in AU. The four angles are not
// ephemeris bodies and are rejected.
func apparentPosition(b Body, jde float64) (lon, lat, dist float64, err error) {
	Δψ, _ := nutation.Nutation(jde)

	switch b {
	case Sun:
		l0, b0, r0, err := heliocentric(vEarth, jde)
		if err != nil {
			return 0, 0, 0, err
		}
		// The Sun seen from Earth is Earth seen from the Sun, turned around.
		λ, β := l0+math.Pi, -b0
		λ, β = toFK5(λ, β, jde)
		// Aberration of the Sun is a pure retardation along the ecliptic
		// (Meeus 25.10); it does not need the general formula.
		λ -= unit.AngleFromSec(20.4898 / r0).Rad()
		λ += Δψ.Rad()
		return norm360(λ * rad2deg), β * rad2deg, r0, nil

	case Moon:
		// moonposition gives geocentric coordinates for the mean equinox of
		// date, with light-time already folded into the theory. Only nutation
		// is left to apply.
		λ, β, Δkm := moonposition.Position(jde)
		return norm360(λ.Deg() + Δψ.Deg()), β.Deg(), Δkm / kmPerAU, nil

	case NorthNode:
		λ := moonposition.TrueNode(jde) + Δψ
		return norm360(λ.Deg()), 0, 0, nil

	case SouthNode:
		lon, lat, dist, err = apparentPosition(NorthNode, jde)
		return norm360(lon + 180), lat, dist, err

	case Pluto:
		return geocentric(plutoHeliocentric, jde)

	default:
		vp, ok := vsopOf[b]
		if !ok {
			return 0, 0, 0, fmt.Errorf("astro: %s has no ephemeris", b)
		}
		return geocentric(func(t float64) (float64, float64, float64, error) {
			return heliocentric(vp, t)
		}, jde)
	}
}

// heliocentricFunc yields a body's heliocentric ecliptic longitude and
// latitude in radians and its radius vector in AU, for the equinox of date.
type heliocentricFunc func(jde float64) (l, b, r float64, err error)

// geocentric converts a heliocentric position to an apparent geocentric
// ecliptic position, iterating for light time and then applying the FK5,
// aberration and nutation corrections (Meeus chapter 33).
func geocentric(pos heliocentricFunc, jde float64) (lon, lat, dist float64, err error) {
	l0, b0, r0, err := heliocentric(vEarth, jde)
	if err != nil {
		return 0, 0, 0, err
	}
	x0, y0, z0 := sphericalToRect(l0, b0, r0)

	var x, y, z, τ float64
	for i := 0; i < 12; i++ {
		l, b, r, err := pos(jde - τ)
		if err != nil {
			return 0, 0, 0, err
		}
		px, py, pz := sphericalToRect(l, b, r)
		x, y, z = px-x0, py-y0, pz-z0
		dist = math.Sqrt(x*x + y*y + z*z)

		next := lightTime * dist
		if math.Abs(next-τ) < 1e-10 {
			τ = next
			break
		}
		τ = next
	}

	λ := math.Atan2(y, x)
	β := math.Atan2(z, math.Hypot(x, y))

	λ, β = toFK5(λ, β, jde)
	dλ, dβ := aberration(λ, β, l0, jde)
	λ, β = λ+dλ, β+dβ

	Δψ, _ := nutation.Nutation(jde)
	λ += Δψ.Rad()

	return norm360(λ * rad2deg), β * rad2deg, dist, nil
}

// plutoHeliocentric wraps Meeus chapter 37, whose result is referred to the
// J2000 ecliptic. Precessing it to the equinox of date matters: by the 1990s
// the two frames differ by roughly a tenth of a degree, enough to move a
// position across a sign boundary.
func plutoHeliocentric(jde float64) (l, b, r float64, err error) {
	lJ2000, bJ2000, r := pluto.Heliocentric(jde)
	from := &coord.Ecliptic{Lon: lJ2000, Lat: bJ2000}
	to := &coord.Ecliptic{}
	precess.EclipticPosition(from, to, 2000, base.JDEToJulianYear(jde), 0, 0)
	return to.Lon.Rad(), to.Lat.Rad(), r, nil
}

func sphericalToRect(l, b, r float64) (x, y, z float64) {
	cb := math.Cos(b)
	return r * cb * math.Cos(l), r * cb * math.Sin(l), r * math.Sin(b)
}

// toFK5 converts ecliptic coordinates from the VSOP87 dynamical frame to the
// FK5 system (Meeus 32.3). The shift is under a tenth of an arcsecond, but it
// is cheap and keeps positions comparable with published ephemerides.
func toFK5(λ, β, jde float64) (float64, float64) {
	t := (jde - j2000) / 36525
	lʹ := λ - (1.397*t+0.00031*t*t)*deg2rad
	s, c := math.Sincos(lʹ)
	dλ := unit.AngleFromSec(-0.09033 + 0.03916*(c+s)*math.Tan(β)).Rad()
	dβ := unit.AngleFromSec(0.03916 * (c - s)).Rad()
	return λ + dλ, β + dβ
}

// aberration returns the annual aberration in ecliptic longitude and latitude
// (Meeus 33.3). Argument earthLon is Earth's heliocentric longitude, from
// which the Sun's geometric longitude follows.
func aberration(λ, β, earthLon, jde float64) (dλ, dβ float64) {
	const κ = 20.49552 / 3600 * deg2rad // constant of aberration, radians

	t := (jde - j2000) / 36525
	sunLon := earthLon + math.Pi
	e := horner(t, 0.016708634, -0.000042037, -0.0000001267)
	perihelion := horner(t, 102.93735, 1.71946, 0.00046) * deg2rad

	sSun, cSun := math.Sincos(sunLon - λ)
	sPeri, cPeri := math.Sincos(perihelion - λ)

	dλ = (-κ*cSun + e*κ*cPeri) / math.Cos(β)
	dβ = -κ * math.Sin(β) * (sSun - e*sPeri)
	return dλ, dβ
}

// longitudeSpeed returns the body's apparent motion in ecliptic longitude in
// degrees per day, from a one-day centred difference. Negative means
// retrograde.
func longitudeSpeed(b Body, jde float64) (float64, error) {
	before, _, _, err := apparentPosition(b, jde-0.5)
	if err != nil {
		return 0, err
	}
	after, _, _, err := apparentPosition(b, jde+0.5)
	if err != nil {
		return 0, err
	}
	return arcSeparation(before, after), nil
}
