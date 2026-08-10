package astro

import (
	"fmt"
	"math"
	"time"

	"github.com/soniakeys/meeus/v3/coord"
	"github.com/soniakeys/unit"
)

// Birth is the input to a natal chart: when and where someone was born. The
// time is held as wall-clock components plus a zone rather than as a
// time.Time, because resolving the two against each other is exactly where
// daylight-saving changes bite, and Compute needs to report on that.
type Birth struct {
	Name string

	Year   int
	Month  time.Month
	Day    int
	Hour   int
	Minute int

	// Zone is the birthplace's IANA time zone; ZoneName is its name, for
	// display.
	Zone     *time.Location
	ZoneName string

	// TimeStatus records how well the time of birth is known, and TimeSource
	// where it came from — "birth certificate", "my mother". Neither affects a
	// single computed number; both change how far the chart can be relied on,
	// which is why they travel with it.
	TimeStatus TimeStatus
	TimeSource string

	// Place and Country are for display only.
	Place   string
	Country string

	// Latitude is positive north, Longitude positive east, in degrees.
	Latitude  float64
	Longitude float64
}

// Placement is where one body sits in the chart.
type Placement struct {
	Body      Body
	Longitude float64 // apparent geocentric ecliptic longitude, degrees
	Latitude  float64 // ecliptic latitude, degrees
	// RightAscension and Declination are the same position in equatorial
	// coordinates, for the same equinox. Declination is what parallels and
	// contra-parallels are read from, and neither can be recovered from the
	// ecliptic longitude alone.
	RightAscension float64
	Declination    float64
	Speed          float64 // degrees of longitude per day
	Retrograde     bool
	Sign           Sign
	DegreeInSign   float64
	House          int
}

// Position renders the placement as e.g. `14°22' Scorpio`.
func (p Placement) Position() string { return FormatPosition(p.Longitude) }

// Degrees renders just the degrees within the sign, e.g. `14°22'`.
func (p Placement) Degrees() string { return FormatDMS(p.DegreeInSign) }

// House cusp descriptions, used by the houses table.
var houseMeanings = [12]string{
	"Self, body, and the first impression you make",
	"Money, possessions, values, and self-worth",
	"Communication, siblings, learning, and short journeys",
	"Home, family, roots, and private life",
	"Creativity, romance, play, and children",
	"Work, routine, service, and health",
	"Partnership, marriage, and open dealings with others",
	"Shared resources, intimacy, death, and transformation",
	"Belief, higher study, travel, and meaning",
	"Career, public standing, and vocation",
	"Friendship, community, and hopes for the future",
	"Solitude, the unconscious, endings, and what is hidden",
}

// House is one of the twelve houses, ready for display.
type House struct {
	Number    int
	Cusp      float64
	Sign      Sign
	Degrees   string
	Meaning   string
	Occupants []Body
}

// Options adjusts what a chart contains. The zero value is what the pages show.
type Options struct {
	// MinorAspects also looks for the six minor aspects. It is off by default:
	// there is no written reading for them, and a chart with twice as many
	// aspects is not twice as informative. A consumer asking for them gets
	// them, and the export says which set was used either way.
	MinorAspects bool
}

// Chart is a fully computed natal chart.
type Chart struct {
	Birth     Birth
	Options   Options
	Local     time.Time
	UTC       time.Time
	JulianDay float64 // Universal Time
	// JDE is the same instant in Terrestrial Time, which is what the ephemeris
	// is actually evaluated at. The two differ by ΔT — about a minute for a
	// twentieth-century birth.
	JDE        float64
	Houses     Houses
	Placements []Placement
	// Derived holds the points computed from the chart's own angles and
	// luminaries — the Part of Fortune and the rest. They are kept apart from
	// Placements because they carry no reading and are not house occupants.
	Derived  []Placement
	byBody   map[Body]Placement
	Houses12 []House
	Aspects  []Aspect
	Warnings []Warning
}

// Placement returns the placement of a body, and whether it was computed.
func (c *Chart) Placement(b Body) (Placement, bool) {
	p, ok := c.byBody[b]
	return p, ok
}

// Compute builds the chart for a birth, with the defaults the pages use.
func Compute(b Birth) (*Chart, error) { return ComputeWith(b, Options{}) }

// ComputeWith builds the chart for a birth under the given options.
func ComputeWith(b Birth, opt Options) (*Chart, error) {
	if b.Zone == nil {
		return nil, fmt.Errorf("astro: birth time zone is not set")
	}
	if b.Latitude < -90 || b.Latitude > 90 {
		return nil, fmt.Errorf("astro: latitude %.4f is out of range", b.Latitude)
	}
	if b.Longitude < -180 || b.Longitude > 180 {
		return nil, fmt.Errorf("astro: longitude %.4f is out of range", b.Longitude)
	}

	local, clockWarning := ResolveLocal(b.Year, b.Month, b.Day, b.Hour, b.Minute, b.Zone)
	utc := local.UTC()
	dayFraction := float64(utc.Hour())/24 +
		float64(utc.Minute())/1440 +
		float64(utc.Second())/86400
	jdUT := julianDay(utc.Year(), int(utc.Month()), float64(utc.Day())+dayFraction)
	jde := jdeFromUT(jdUT)

	c := &Chart{
		Birth:     b,
		Options:   opt,
		Local:     local,
		UTC:       utc,
		JulianDay: jdUT,
		JDE:       jde,
		Houses:    ComputeHouses(Placidus, jdUT, b.Latitude, b.Longitude),
		byBody:    make(map[Body]Placement),
	}
	if c.Houses.Note != "" {
		c.Warnings = append(c.Warnings, Warning{
			Code:           WarnPlacidusUnavailable,
			Severity:       SeverityWarning,
			Message:        c.Houses.Note,
			AffectedFields: []string{FieldHouses, FieldAngles},
		})
	}
	if clockWarning != nil {
		c.Warnings = append(c.Warnings, *clockWarning)
	}
	if w := b.timeWarning(); w != nil {
		c.Warnings = append(c.Warnings, *w)
	}

	all := append(append([]Body{}, TableBodies...), PointBodies...)
	for _, body := range all {
		p, err := c.placementOf(body, jde)
		if err != nil {
			return nil, err
		}
		c.Placements = append(c.Placements, p)
		c.byBody[body] = p
	}

	c.Houses12 = c.buildHouses()
	c.Aspects = c.buildAspects()

	// The derived points come last: each of them is arithmetic on the angles and
	// the luminaries, so they need the rest of the chart to already stand. They
	// are registered by body so that Placement finds them, but stay out of
	// Placements, which is what the tables and the house occupants are built
	// from.
	c.Derived = c.derive(jde)
	for _, p := range c.Derived {
		c.byBody[p.Body] = p
	}

	c.Warnings = append(c.Warnings, c.marginalWarnings()...)
	return c, nil
}

func (c *Chart) placementOf(body Body, jde float64) (Placement, error) {
	var lon, lat, speed float64

	switch body {
	case Ascendant:
		lon = c.Houses.Ascendant
	case Descendant:
		lon = c.Houses.Descendant()
	case Midheaven:
		lon = c.Houses.Midheaven
	case ImumCoeli:
		lon = c.Houses.ImumCoeli()
	default:
		var err error
		lon, lat, _, err = apparentPosition(body, jde)
		if err != nil {
			return Placement{}, err
		}
		if speed, err = longitudeSpeed(body, jde); err != nil {
			return Placement{}, err
		}
	}

	p := c.placementAt(body, lon, lat)
	p.Speed = speed
	p.Retrograde = speed < 0
	return p, nil
}

// placementAt situates an ecliptic position in the chart: which sign and degree
// it falls in, which house holds it, and where it sits on the equator.
//
// The equatorial pair is computed here rather than left to the caller because it
// depends on the obliquity of the day, which the house division already worked
// out and which nothing outside this package should have to know about.
func (c *Chart) placementAt(body Body, lon, lat float64) Placement {
	sε, cε := math.Sincos(c.Houses.Obliquity * deg2rad)
	α, δ := coord.EclToEq(unit.AngleFromDeg(lon), unit.AngleFromDeg(lat), sε, cε)

	return Placement{
		Body:           body,
		Longitude:      lon,
		Latitude:       lat,
		RightAscension: norm360(α.Deg()),
		Declination:    δ.Deg(),
		Sign:           SignOf(lon),
		DegreeInSign:   DegreeInSign(lon),
		House:          c.Houses.HouseOf(lon),
	}
}

func (c *Chart) buildHouses() []House {
	houses := make([]House, 12)
	for i := range houses {
		cusp := c.Houses.Cusps[i]
		houses[i] = House{
			Number:  i + 1,
			Cusp:    cusp,
			Sign:    SignOf(cusp),
			Degrees: FormatDMS(DegreeInSign(cusp)),
			Meaning: houseMeanings[i],
		}
	}
	// The angles are the house cusps themselves, so listing them as occupants
	// would be noise; only the moving bodies are placed.
	for _, p := range c.Placements {
		if p.Body.IsAngle() {
			continue
		}
		h := &houses[p.House-1]
		h.Occupants = append(h.Occupants, p.Body)
	}
	return houses
}

// Aspect is a significant angular relationship between two chart points.
type Aspect struct {
	A, B  Body
	Name  string
	Glyph string
	Angle float64 // the exact angle the aspect is named for
	Orb   float64 // how far the pair is from exact, in degrees
	// Separation is the actual angle between the pair, in [0, 180].
	Separation float64
}

// Exactness renders the orb as e.g. `2°14'`.
func (a Aspect) Exactness() string { return FormatDMS(a.Orb) }

// Nature groups the aspect by the kind of relationship it describes.
func (a Aspect) Nature() string {
	switch a.Name {
	case "Trine", "Sextile", "Quintile", "Biquintile", "Semisextile":
		return "harmonious"
	case "Square", "Opposition", "Semisquare", "Sesquisquare":
		return "challenging"
	case "Quincunx":
		return "disjunct"
	default:
		return "conjunction"
	}
}

// Minor reports whether the aspect is one of the minor ones, which are only
// looked for when they are asked for.
func (a Aspect) Minor() bool {
	for _, k := range minorAspects {
		if k.name == a.Name {
			return true
		}
	}
	return false
}

type aspectKind struct {
	name  string
	glyph string
	angle float64
	orb   float64
	// bonus is the extra orb allowed when the Sun or the Moon is one of the
	// pair. It is smaller for the minor aspects, in proportion to their orbs.
	bonus float64
	minor bool
}

// AspectKind describes one of the aspects a chart looks for, so that a caller
// exporting a chart can state the rules it was built under.
type AspectKind struct {
	Name  string
	Glyph string
	Angle float64
	// Orb is the orb allowed for an ordinary pair of bodies, and LuminaryOrb
	// the wider orb allowed when the Sun or the Moon is one of them.
	Orb         float64
	LuminaryOrb float64
	// Minor is true for the aspects that are only searched on request.
	Minor bool
}

// LuminaryOrbBonus is the extra orb allowed on a major aspect when the Sun or
// the Moon is one of the pair. The luminaries carry more weight, so they are
// given more room — the usual convention.
const LuminaryOrbBonus = 2.0

// AspectSet returns the aspects a chart looks for under the given options, with
// the orbs allowed for each.
func AspectSet(opt Options) []AspectKind {
	kinds := aspectKindsFor(opt)
	out := make([]AspectKind, 0, len(kinds))
	for _, k := range kinds {
		out = append(out, AspectKind{
			Name: k.name, Glyph: k.glyph, Angle: k.angle,
			Orb: k.orb, LuminaryOrb: k.orb + k.bonus,
			Minor: k.minor,
		})
	}
	return out
}

// The five Ptolemaic aspects, with the orbs most commonly used for natal work.
var majorAspects = []aspectKind{
	{name: "Conjunction", glyph: "☌", angle: 0, orb: 8, bonus: LuminaryOrbBonus},
	{name: "Opposition", glyph: "☍", angle: 180, orb: 8, bonus: LuminaryOrbBonus},
	{name: "Trine", glyph: "△", angle: 120, orb: 7, bonus: LuminaryOrbBonus},
	{name: "Square", glyph: "□", angle: 90, orb: 7, bonus: LuminaryOrbBonus},
	{name: "Sextile", glyph: "⚹", angle: 60, orb: 5, bonus: LuminaryOrbBonus},
}

// The minor aspects, off unless asked for. They are read much more tightly than
// the majors — a quincunx four degrees out is not a quincunx — and there is no
// written reading for any of them, so a chart that includes them is giving a
// consumer geometry rather than an interpretation.
//
// The orbs are not a uniform allowance: the quintile family is given least,
// which is both the usual practice and a necessity, since a biquintile at 144°
// and a quincunx at 150° are only six degrees apart and wider orbs would leave a
// band of separations that could be read as either.
var minorAspects = []aspectKind{
	{name: "Quincunx", glyph: "⚻", angle: 150, orb: 3, bonus: 1, minor: true},
	{name: "Semisextile", glyph: "⚺", angle: 30, orb: 2, bonus: 1, minor: true},
	{name: "Semisquare", glyph: "∠", angle: 45, orb: 2, bonus: 1, minor: true},
	{name: "Sesquisquare", glyph: "⚼", angle: 135, orb: 2, bonus: 1, minor: true},
	{name: "Quintile", glyph: "Q", angle: 72, orb: 1.5, bonus: 0.5, minor: true},
	{name: "Biquintile", glyph: "bQ", angle: 144, orb: 1.5, bonus: 0.5, minor: true},
}

// aspectKindsFor returns the aspects to search, majors first so that a pair
// close to both a major and a minor aspect is reported as the major one.
func aspectKindsFor(opt Options) []aspectKind {
	if !opt.MinorAspects {
		return majorAspects
	}
	return append(append([]aspectKind{}, majorAspects...), minorAspects...)
}

// AspectNames returns the name of every aspect a chart reports by default, so
// that callers holding written text for aspects can check they have covered the
// set. The minor aspects are excluded: none of them has a reading.
func AspectNames() []string {
	names := make([]string, 0, len(majorAspects))
	for _, k := range majorAspects {
		names = append(names, k.name)
	}
	return names
}

// AspectBodies are the points allowed to form aspects. The Descendant and IC
// are omitted because they would only duplicate their opposite angle.
var AspectBodies = []Body{
	Sun, Moon, Mercury, Venus, Mars, Jupiter, Saturn, Uranus, Neptune, Pluto,
	Ascendant, Midheaven,
}

func (c *Chart) buildAspects() []Aspect {
	kinds := aspectKindsFor(c.Options)
	var out []Aspect
	for i := 0; i < len(AspectBodies); i++ {
		pa, ok := c.byBody[AspectBodies[i]]
		if !ok {
			continue
		}
		for j := i + 1; j < len(AspectBodies); j++ {
			pb, ok := c.byBody[AspectBodies[j]]
			if !ok {
				continue
			}
			separation := math.Abs(arcSeparation(pa.Longitude, pb.Longitude))
			for _, k := range kinds {
				orb := math.Abs(separation - k.angle)
				if orb <= k.orb+luminaryBonus(pa.Body, pb.Body, k.bonus) {
					out = append(out, Aspect{
						A: pa.Body, B: pb.Body,
						Name: k.name, Glyph: k.glyph, Angle: k.angle, Orb: orb,
						Separation: separation,
					})
					break
				}
			}
		}
	}
	return out
}

// luminaryBonus is the extra orb a pair earns for including the Sun or the Moon.
func luminaryBonus(a, b Body, bonus float64) float64 {
	if a == Sun || a == Moon || b == Sun || b == Moon {
		return bonus
	}
	return 0
}

// OrbAllowed returns the widest orb this aspect could have had and still been
// listed. It is what makes an aspect near the limit recognisable as marginal:
// two programs will list different aspects for the same chart if their orbs
// differ, and the difference is invisible without this.
func (c *Chart) OrbAllowed(a Aspect) float64 {
	for _, k := range aspectKindsFor(c.Options) {
		if k.name == a.Name {
			return k.orb + luminaryBonus(a.A, a.B, k.bonus)
		}
	}
	return 0
}

// Applying reports whether the two bodies were still closing on the exact
// aspect at birth, and whether that could be decided at all.
//
// It cannot be decided for the Ascendant or the Midheaven. Their motion comes
// from the Earth's rotation rather than from the ephemeris, and the chart holds
// no rate for them; saying "separating" for want of a speed would be a
// fabrication, so the second return value says so instead.
func (c *Chart) Applying(a Aspect) (applying, known bool) {
	pa, okA := c.byBody[a.A]
	pb, okB := c.byBody[a.B]
	if !okA || !okB || a.A.IsAngle() || a.B.IsAngle() {
		return false, false
	}

	// A short step forward at the current rates. Long enough that the orb
	// visibly moves, short enough that a linear extrapolation still holds even
	// for the Moon.
	const step = 0.02 // days
	orbAt := func(la, lb float64) float64 {
		return math.Abs(math.Abs(arcSeparation(la, lb)) - a.Angle)
	}
	now := orbAt(pa.Longitude, pb.Longitude)
	later := orbAt(pa.Longitude+pa.Speed*step, pb.Longitude+pb.Speed*step)
	return later < now, true
}

// Sect reports whether the Sun stood above the horizon at birth — a day chart
// — or below it. Several placements are read differently depending on which,
// so it is worth stating rather than leaving to be inferred from the Sun's
// house.
func (c *Chart) Sect() (string, bool) {
	if _, ok := c.byBody[Sun]; !ok {
		return "", false
	}
	if c.isDay() {
		return "day", true
	}
	return "night", true
}

// isDay reports whether the Sun stood above the horizon, which is to say in one
// of the houses from the seventh to the twelfth. A chart with no Sun is treated
// as nocturnal, but no caller reaches that: Sect guards it and the derived
// points are only computed when the Sun is present.
func (c *Chart) isDay() bool {
	sun, ok := c.byBody[Sun]
	return ok && sun.House >= 7
}

// MoonPhase is the Moon's phase at birth, taken from its elongation from the
// Sun. It is named twice, because the two traditions divide the cycle
// differently and each name is wrong in the other's terms.
type MoonPhase struct {
	// Name is the phase as an almanac would give it, so that it always agrees
	// with Illumination.
	Name string
	// Lunation is the phase as natal astrology divides it, after Rudhyar. The
	// eight names carry meanings of their own and are what a reading will use.
	Lunation string
	// Angle is the Moon's longitude minus the Sun's, in [0, 360): zero at the
	// new moon, 180 at the full.
	Angle float64
	// Illumination is the lit fraction of the disc, 0 to 1.
	Illumination float64
}

// lunationPhases are the eight phases of the lunation cycle, each a 45° eighth
// beginning at the aspect it is named for.
var lunationPhases = [8]string{
	"New", "Crescent", "First Quarter", "Gibbous",
	"Full", "Disseminating", "Last Quarter", "Balsamic",
}

// moonPhaseName gives the almanac name for an elongation.
//
// The quarters and the syzygies get narrow bands and the crescents and gibbous
// phases wide ones, rather than an even eighth each. An even division would
// call a 61%-lit Moon the "Last Quarter", which is a quarter only in the sense
// that it is nearer to one than to anything else.
func moonPhaseName(angle float64) string {
	const exact = 6 // roughly half a day either side of the aspect
	switch {
	case angle < exact || angle >= 360-exact:
		return "New Moon"
	case math.Abs(angle-90) < exact:
		return "First Quarter"
	case math.Abs(angle-180) < exact:
		return "Full Moon"
	case math.Abs(angle-270) < exact:
		return "Last Quarter"
	case angle < 90:
		return "Waxing Crescent"
	case angle < 180:
		return "Waxing Gibbous"
	case angle < 270:
		return "Waning Gibbous"
	default:
		return "Waning Crescent"
	}
}

// MoonPhase returns the lunar phase of the chart.
func (c *Chart) MoonPhase() (MoonPhase, bool) {
	sun, okS := c.byBody[Sun]
	moon, okM := c.byBody[Moon]
	if !okS || !okM {
		return MoonPhase{}, false
	}
	angle := norm360(moon.Longitude - sun.Longitude)
	return MoonPhase{
		Name:     moonPhaseName(angle),
		Lunation: lunationPhases[int(angle/45)%8],
		Angle:    angle,
		// The elongation and the phase angle are supplements, so the usual
		// (1 + cos i)/2 becomes a minus here.
		Illumination: (1 - math.Cos(angle*deg2rad)) / 2,
	}, true
}

// ResolveLocal turns wall-clock components in a zone into an instant, and
// reports when that mapping is not one-to-one.
//
// On the two days a year the clocks change, a wall-clock time either happens
// twice or not at all. time.Date resolves both cases silently, but for a birth
// chart the two readings are an hour apart — enough to move the Ascendant by
// fifteen degrees — so the ambiguity has to be surfaced rather than guessed at.
//
// The returned warning is nil when the mapping was unambiguous.
func ResolveLocal(year int, month time.Month, day, hour, minute int, loc *time.Location) (time.Time, *Warning) {
	// The same wall clock read as if it were UTC. Subtracting a candidate
	// offset from this gives the instant that would display it.
	naive := time.Date(year, month, day, hour, minute, 0, 0, time.UTC)

	// Collect the offsets in force around this date; a day either side
	// comfortably brackets any transition.
	var offsets []int
	seen := map[int]bool{}
	for _, delta := range []time.Duration{-26 * time.Hour, 0, 26 * time.Hour} {
		if _, off := naive.Add(delta).In(loc).Zone(); !seen[off] {
			seen[off] = true
			offsets = append(offsets, off)
		}
	}

	var matches []time.Time
	for _, off := range offsets {
		candidate := naive.Add(-time.Duration(off) * time.Second).In(loc)
		cy, cm, cd := candidate.Date()
		ch, cmin, _ := candidate.Clock()
		if cy == year && cm == month && cd == day && ch == hour && cmin == minute {
			matches = append(matches, candidate)
		}
	}

	switch len(matches) {
	case 0:
		return time.Date(year, month, day, hour, minute, 0, 0, loc), &Warning{
			Code:     WarnClockChangeSkipped,
			Severity: SeverityWarning,
			Message: "The clocks went forward at this place on this date, so the time given never occurred. " +
				"The chart uses the moment the clocks changed to.",
			AffectedFields: []string{FieldUTC, FieldAngles, FieldHouses},
		}
	case 1:
		return matches[0], nil
	default:
		earliest := matches[0]
		for _, m := range matches[1:] {
			if m.Before(earliest) {
				earliest = m
			}
		}
		return earliest, &Warning{
			Code:     WarnClockChangeRepeated,
			Severity: SeverityWarning,
			Message: "The clocks went back at this place on this date, so this time occurred twice. " +
				"The chart uses the first occurrence; if the birth was after the change, the angles and houses shift by about fifteen degrees.",
			AffectedFields: []string{FieldUTC, FieldAngles, FieldHouses},
		}
	}
}
