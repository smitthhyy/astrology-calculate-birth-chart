package astro

import (
	"fmt"
	"math"
)

// Warning is something the reader should know about how a chart was arrived at.
//
// A warning is data, not decoration. Whether a chart can be trusted depends on
// things that do not show up anywhere in the numbers — a doubtful time zone, a
// house system that had to be substituted, a birth time nobody wrote down — and
// a program receiving the chart needs to be able to act on those without
// reading English. Hence the code and the list of fields each warning casts
// doubt on, beside the sentence a person reads.
type Warning struct {
	// Code is a stable identifier. The message may be reworded; this may not.
	Code string
	// Severity is "warning" for anything that could make the chart wrong, and
	// "info" for something worth noticing that is nonetheless correct.
	Severity string
	// Message is the sentence shown to a reader.
	Message string
	// AffectedFields are dotted paths into the exported document, naming what
	// should be treated with less confidence.
	AffectedFields []string
}

// Warning severities.
const (
	SeverityWarning = "warning"
	SeverityInfo    = "info"
)

// Warning codes. A consumer switching on these will not be broken by a change
// of wording.
const (
	// WarnPlacidusUnavailable: inside the polar circles Placidus has no
	// solution and Whole Sign houses were used instead.
	WarnPlacidusUnavailable = "PLACIDUS_UNAVAILABLE"
	// WarnClockChangeSkipped: the wall-clock time given never occurred, because
	// the clocks went forward through it.
	WarnClockChangeSkipped = "CLOCK_CHANGE_SKIPPED_TIME"
	// WarnClockChangeRepeated: the wall-clock time given occurred twice, and
	// the earlier of the two was used.
	WarnClockChangeRepeated = "CLOCK_CHANGE_REPEATED_TIME"
	// WarnTimeZoneMismatch: the chosen zone is not the zone of the coordinates.
	WarnTimeZoneMismatch = "TIME_ZONE_MISMATCH"
	// WarnTimeZoneInferred: no zone was chosen and the coordinates were not
	// taken from a known place, so the zone of the nearest one was used.
	WarnTimeZoneInferred = "TIME_ZONE_INFERRED"
	// WarnBirthTimeApproximate: the birth time was given as an estimate.
	WarnBirthTimeApproximate = "BIRTH_TIME_APPROXIMATE"
	// WarnBirthTimeUnknown: the birth time is not known at all.
	WarnBirthTimeUnknown = "BIRTH_TIME_UNKNOWN"
	// WarnBirthTimeRectified: the birth time was arrived at by rectification
	// rather than from a record.
	WarnBirthTimeRectified = "BIRTH_TIME_RECTIFIED"
	// WarnPointNearCusp: a point lies close enough to a house cusp that a
	// slightly different birth time would move it to the next house.
	WarnPointNearCusp = "POINT_NEAR_HOUSE_CUSP"
	// WarnAspectNearOrbLimit: an aspect is close to the widest orb allowed, so
	// software using a tighter orb will not list it.
	WarnAspectNearOrbLimit = "ASPECT_NEAR_ORB_LIMIT"
)

// Fields warnings point at. These are paths in the exported document, exported
// so that a caller adding a warning of its own names the same places this
// package does; two spellings of the same path would defeat the purpose.
const (
	FieldAngles    = "calculatedChart.angles"
	FieldHouses    = "calculatedChart.houses"
	FieldPoints    = "calculatedChart.points"
	FieldAspects   = "calculatedChart.aspects"
	FieldTimeZone  = "birth.timeZone"
	FieldUTC       = "birth.utc"
	FieldMoonPhase = "calculatedChart.moonPhase"
)

// WarningFields is every path a warning can name, for a consumer that wants to
// check it understands them all.
func WarningFields() []string {
	return []string{
		FieldAngles, FieldHouses, FieldPoints, FieldAspects,
		FieldTimeZone, FieldUTC, FieldMoonPhase,
	}
}

// cuspProximity is how close to a house cusp a point has to be before the
// placement is worth flagging. One degree is roughly four minutes of birth
// time, which is well inside the uncertainty of an ordinary recorded time.
const cuspProximity = 1.0

// orbProximity is how close to the widest orb allowed an aspect has to be
// before it is worth flagging as marginal.
const orbProximity = 1.0

// marginalWarnings reports the placements and aspects that a small change in
// the input would alter. Both are severity "info": nothing here is wrong, but a
// consumer comparing this chart against another program's needs to know which
// entries sit on a boundary.
func (c *Chart) marginalWarnings() []Warning {
	var out []Warning

	for _, p := range c.Placements {
		if p.Body.IsAngle() {
			continue // an angle is a cusp; being on one is not news
		}
		for i, cusp := range c.Houses.Cusps {
			gap := arcSeparation(cusp, p.Longitude)
			if math.Abs(gap) > cuspProximity {
				continue
			}
			out = append(out, Warning{
				Code:     WarnPointNearCusp,
				Severity: SeverityInfo,
				Message: fmt.Sprintf(
					"%s lies %s from the cusp of the %s house, so a birth time a few minutes out would place it in the %s house instead.",
					p.Body.Name(), FormatDMS(math.Abs(gap)), ordinal(i+1), ordinal(otherSideOf(i+1, gap))),
				AffectedFields: []string{FieldPoints, FieldHouses},
			})
		}
	}

	for _, a := range c.Aspects {
		allowed := c.OrbAllowed(a)
		if allowed-a.Orb > orbProximity {
			continue
		}
		out = append(out, Warning{
			Code:     WarnAspectNearOrbLimit,
			Severity: SeverityInfo,
			Message: fmt.Sprintf(
				"The %s between %s and %s is %s wide, close to the %.0f° limit allowed here, so software using tighter orbs will not list it.",
				a.Name, a.A.Name(), a.B.Name(), a.Exactness(), allowed),
			AffectedFields: []string{FieldAspects},
		})
	}
	return out
}

// otherSideOf names the house a point would fall into if it crossed the cusp it
// is sitting beside. A positive gap means the point has already passed the cusp,
// so the house it would move back into is the one before.
func otherSideOf(house int, gap float64) int {
	if gap > 0 {
		return (house+10)%12 + 1
	}
	return house
}

// ordinal renders a house number for a sentence.
func ordinal(n int) string {
	suffix := "th"
	if n%100 < 11 || n%100 > 13 {
		switch n % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return fmt.Sprintf("%d%s", n, suffix)
}
