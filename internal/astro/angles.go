package astro

import (
	"fmt"
	"math"
)

// j2000 is the Julian Day of the J2000.0 epoch, 2000 January 1.5 TT.
const j2000 = 2451545.0

const (
	deg2rad = math.Pi / 180
	rad2deg = 180 / math.Pi
)

// pmod returns x reduced to [0, y) — Go's % and math.Mod keep the sign of the
// dividend, which is never what angle arithmetic wants.
func pmod(x, y float64) float64 {
	r := math.Mod(x, y)
	if r < 0 {
		r += y
	}
	return r
}

// norm360 reduces an angle in degrees to [0, 360).
func norm360(deg float64) float64 { return pmod(deg, 360) }

// arcSeparation returns the shortest signed angular distance from a to b, in
// degrees, in the range (-180, 180].
func arcSeparation(a, b float64) float64 {
	d := norm360(b - a)
	if d > 180 {
		d -= 360
	}
	return d
}

// Sign is a sign of the tropical zodiac, 0 = Aries through 11 = Pisces.
type Sign int

// The twelve signs, in zodiacal order.
const (
	Aries Sign = iota
	Taurus
	Gemini
	Cancer
	Leo
	Virgo
	Libra
	Scorpio
	Sagittarius
	Capricorn
	Aquarius
	Pisces
)

var signNames = [12]string{
	"Aries", "Taurus", "Gemini", "Cancer", "Leo", "Virgo",
	"Libra", "Scorpio", "Sagittarius", "Capricorn", "Aquarius", "Pisces",
}

var signGlyphs = [12]string{
	"♈", "♉", "♊", "♋", "♌", "♍",
	"♎", "♏", "♐", "♑", "♒", "♓",
}

// signKeys are the lower-case identifiers used in interpretations.json.
var signKeys = [12]string{
	"aries", "taurus", "gemini", "cancer", "leo", "virgo",
	"libra", "scorpio", "sagittarius", "capricorn", "aquarius", "pisces",
}

func (s Sign) String() string { return signNames[s%12] }

// Number returns the sign's place in zodiacal order, Aries = 1.
func (s Sign) Number() int { return int(s%12) + 1 }

// Glyph returns the sign's astrological symbol.
func (s Sign) Glyph() string { return signGlyphs[s%12] }

// Key returns the sign's identifier as used in the interpretation data.
func (s Sign) Key() string { return signKeys[s%12] }

// Element returns Fire, Earth, Air or Water.
func (s Sign) Element() string {
	return [4]string{"Fire", "Earth", "Air", "Water"}[s%4]
}

// Modality returns Cardinal, Fixed or Mutable.
func (s Sign) Modality() string {
	return [3]string{"Cardinal", "Fixed", "Mutable"}[s%3]
}

// Polarity returns Positive for the fire and air signs and Negative for the
// earth and water signs — the older masculine/feminine division, which alternates
// around the zodiac.
func (s Sign) Polarity() string {
	if s%2 == 0 {
		return "Positive"
	}
	return "Negative"
}

// RulershipSystem names which of the two rulership schemes a chart is read
// under. Every sign has two rulers and the reading changes with the choice, so
// it has to be stated rather than assumed.
type RulershipSystem string

const (
	// TraditionalRulership assigns the seven visible planets only, the scheme
	// in use before Uranus was found.
	TraditionalRulership RulershipSystem = "traditional"
	// ModernRulership gives Scorpio to Pluto, Aquarius to Uranus and Pisces to
	// Neptune. This is what the readings here are written for.
	ModernRulership RulershipSystem = "modern"
)

// Rulership is the scheme this application reads charts under.
const Rulership = ModernRulership

// TraditionalRuler returns the sign's classical ruler, drawn from the seven
// planets visible to the naked eye. Mars keeps Scorpio, Saturn keeps Aquarius
// and Jupiter keeps Pisces.
func (s Sign) TraditionalRuler() string {
	return [12]string{
		"Mars", "Venus", "Mercury", "Moon", "Sun", "Mercury",
		"Venus", "Mars", "Jupiter", "Saturn", "Saturn", "Jupiter",
	}[s%12]
}

// ModernRuler returns the sign's ruler once the three outer planets are given
// the rulerships they are usually assigned.
func (s Sign) ModernRuler() string {
	return [12]string{
		"Mars", "Venus", "Mercury", "Moon", "Sun", "Mercury",
		"Venus", "Pluto", "Jupiter", "Saturn", "Uranus", "Neptune",
	}[s%12]
}

// Ruler returns the ruling planet under the scheme this application uses, which
// is the modern one. Both are available, so a consumer reading traditionally
// can take the other.
func (s Sign) Ruler() string { return s.ModernRuler() }

// SignOf returns the sign containing ecliptic longitude lon (degrees).
func SignOf(lon float64) Sign { return Sign(int(norm360(lon)/30) % 12) }

// DegreeInSign returns the position of lon within its sign, in degrees.
func DegreeInSign(lon float64) float64 { return norm360(lon) - 30*math.Floor(norm360(lon)/30) }

// DecanOf returns which third of its sign lon falls in, 1 to 3. Each decan is
// ten degrees wide and carries its own flavour in most reading traditions.
func DecanOf(lon float64) int { return int(DegreeInSign(lon)/10) + 1 }

// SplitDMS breaks a degree value into whole degrees, whole arcminutes and the
// arcseconds left over, the arcseconds keeping their fraction.
//
// Both smaller parts are truncated rather than rounded, so that the three
// together are always the same angle as the input, never a fraction more. The
// value is expected to be non-negative — a degree within a sign, or an orb —
// and its magnitude is taken if it is not.
func SplitDMS(deg float64) (degrees, minutes int, seconds float64) {
	total := math.Abs(deg) * 3600
	degrees = int(total / 3600)
	minutes = int(math.Mod(total, 3600) / 60)
	return degrees, minutes, math.Mod(total, 60)
}

// FormatDMS renders a degrees-within-sign value as e.g. `14°22'`. Minutes are
// truncated rather than rounded, as ephemerides conventionally do, so a
// position never reads as the 30° that belongs to the next sign.
func FormatDMS(deg float64) string {
	d, m, _ := SplitDMS(deg)
	return fmt.Sprintf("%d°%02d'", d, m)
}

// FormatDMSSeconds renders a degrees-within-sign value to the arcsecond, as
// e.g. `14°22'37"`.
//
// Truncating is what keeps this honest at the arcminute: rounding 29°59'40" up
// would give 30°00', a degree that belongs to the next sign. Carried down to
// the arcsecond the same rule loses at most one second, so anything comparing
// this against another ephemeris agrees to the last digit shown.
func FormatDMSSeconds(deg float64) string {
	d, m, s := SplitDMS(deg)
	return fmt.Sprintf("%d°%02d'%02d\"", d, m, int(s))
}

// FormatPosition renders an absolute ecliptic longitude as e.g. `14°22' Scorpio`.
func FormatPosition(lon float64) string {
	return fmt.Sprintf("%s %s", FormatDMS(DegreeInSign(lon)), SignOf(lon))
}

// FormatPositionSeconds renders an absolute ecliptic longitude to the
// arcsecond, as e.g. `14°22'37" Scorpio`.
func FormatPositionSeconds(lon float64) string {
	return fmt.Sprintf("%s %s", FormatDMSSeconds(DegreeInSign(lon)), SignOf(lon))
}
