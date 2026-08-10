package astro

// What this package is, in the terms a consumer of an exported chart needs.
//
// Two programs can cast the same birth and disagree, and be right both times:
// they used different tables, a different house system, a different rulership
// scheme. None of that is recoverable from a longitude. So the conventions and
// the provenance are published beside the numbers, and everything here exists to
// be read out into an export rather than used inside the calculation.

// EngineVersion is the version of the calculation itself, as opposed to the
// application around it or the format it is written out in.
//
// It changes when a computed number could change: a new ephemeris source, a
// corrected series, a different orb, a change to how houses are divided. It does
// not change for a rewording, a new field in the export, or anything that leaves
// every position where it was. A consumer that has cached a chart can compare
// this against the version it was cast under and know whether recasting it would
// give the same answer.
const EngineVersion = "1.0.0"

// Zodiac names the frame the signs are measured in. The tropical zodiac begins
// at the vernal equinox of the date, so it drifts against the constellations;
// the sidereal one does not. This application is tropical throughout.
const Zodiac = "tropical"

// ObserverPerspective is where the chart is drawn from. Geocentric means the
// positions are as they would be seen from the centre of the Earth, which is
// the convention in natal astrology; topocentric would correct for the
// observer's own position on the surface, a difference that reaches about a
// degree for the Moon and is negligible for everything else.
const ObserverPerspective = "geocentric"

// NodeType says which lunar nodes the chart carries. The true node is the
// instantaneous intersection of the Moon's orbit with the ecliptic, which
// oscillates about a degree and a half either side of the mean node as the Sun
// pulls on the orbit. Software using mean nodes will differ from this by up to
// that much.
const NodeType = "true"

// The reference frame every position in a chart is given in. All three parts
// matter: a longitude referred to J2000 rather than to the date would be nearly
// half a degree out for a twentieth-century birth.
const (
	CoordinateFrame    = "geocentric"
	CoordinateEquinox  = "true equinox of date"
	CoordinateEcliptic = "true ecliptic of date"
)

// The convention by which a point is assigned to a house. Some astrologers move
// a planet forward into the following house when it falls within a few degrees
// of the next cusp; this application does not, and says so rather than leaving
// it to be inferred from a placement that happens to look wrong.
const (
	CuspConvention       = "a point belongs to the house beginning at the cusp below it"
	CuspAllowanceDegrees = 0.0
)

// Ephemeris is one of the sources a chart's positions are computed from.
type Ephemeris struct {
	// Key is a stable machine-readable identifier for the source.
	Key string
	// Name is what the source is called in the literature.
	Name string
	// Covers says which bodies come from it.
	Covers string
	// Source is the publication the theory comes from.
	Source string
	// Version identifies this implementation of it, which is what actually
	// produced the numbers. The theory is fixed; an implementation of it can be
	// corrected.
	Version string
}

// Ephemerides returns the astronomical sources behind a chart, so that an
// export can name them and a consumer comparing two charts can tell whether a
// disagreement is a bug or a difference of tables.
func Ephemerides() []Ephemeris {
	return []Ephemeris{
		{
			Key:     "vsop87d",
			Name:    "VSOP87D",
			Covers:  "Mercury, Venus, Mars, Jupiter, Saturn, Uranus, Neptune, and the Earth for the Sun",
			Source:  "Bretagnon & Francou 1988",
			Version: "full published series, embedded coefficient tables, " + EngineVersion,
		},
		{
			Key:     "elp2000-82",
			Name:    "ELP-2000/82",
			Covers:  "the Moon and the true lunar node",
			Source:  "Chapront-Touzé & Chapront, as abridged in Meeus, Astronomical Algorithms, chapter 47",
			Version: "Meeus abridgement, " + EngineVersion,
		},
		{
			Key:     "meeus-pluto",
			Name:    "Meeus chapter 37",
			Covers:  "Pluto, precessed from J2000 to the equinox of date",
			Source:  "Meeus, Astronomical Algorithms, chapter 37",
			Version: "valid 1885–2099, " + EngineVersion,
		},
	}
}

// TimeStatus records how well the time of birth is known. Everything that turns
// on the exact minute — the angles, the house cusps, and to a lesser extent the
// Moon's degree — is only as good as this, and a program receiving a chart has
// no other way to tell a time read off a birth certificate from one somebody
// guessed at.
type TimeStatus string

const (
	// TimeUnstated is the default: nothing was said about where the time came
	// from. It is not the same as "recorded", and must not be treated as such.
	TimeUnstated TimeStatus = "unstated"
	// TimeRecorded means the time was taken from a record made at the time.
	TimeRecorded TimeStatus = "recorded"
	// TimeApproximate means the time is remembered or estimated.
	TimeApproximate TimeStatus = "approximate"
	// TimeRectified means the time was worked backwards from events in the life
	// rather than from any record of the birth.
	TimeRectified TimeStatus = "rectified"
	// TimeUnknown means the time is not known at all, and whatever was entered
	// stands in for it — conventionally noon.
	TimeUnknown TimeStatus = "unknown"
)

// timeStatuses is every value in the order they run from best known to least.
var timeStatuses = []TimeStatus{
	TimeRecorded, TimeApproximate, TimeRectified, TimeUnknown, TimeUnstated,
}

// ParseTimeStatus reads a status from a form value, falling back to TimeUnstated
// for anything it does not recognise. Guessing at a better default would be
// worse than admitting nothing was said.
func ParseTimeStatus(s string) TimeStatus {
	for _, candidate := range timeStatuses {
		if s == string(candidate) {
			return candidate
		}
	}
	return TimeUnstated
}

// TimeStatuses returns every status, best-known first, for building a form.
func TimeStatuses() []TimeStatus { return append([]TimeStatus{}, timeStatuses...) }

// Label is the status as a sentence fragment a reader would recognise.
func (t TimeStatus) Label() string {
	switch t {
	case TimeRecorded:
		return "From a record made at the time"
	case TimeApproximate:
		return "Remembered or estimated"
	case TimeRectified:
		return "Rectified from events in the life"
	case TimeUnknown:
		return "Not known"
	default:
		return "Not stated"
	}
}

// Rectified reports whether the time was arrived at by rectification, which the
// export states separately because a consumer may want to exclude such charts
// from any statistical use without caring about the other distinctions.
func (t TimeStatus) Rectified() bool { return t == TimeRectified }

// AccuracySeconds returns how close to the truth the birth time can be taken to
// be, and false when that cannot be said.
//
// Only a recorded time gets a figure, and the figure is the granularity of the
// input rather than a judgement about the record: the form accepts hours and
// minutes, so even a time copied exactly off a certificate is known to within a
// minute and no better. For every other status the honest answer is that nothing
// is known about the accuracy, which is not the same as its being good.
func (t TimeStatus) AccuracySeconds() (int, bool) {
	if t == TimeRecorded {
		return 60, true
	}
	return 0, false
}

// timeWarning is what the chart should say about a doubtful birth time, or nil
// when there is nothing to say.
func (b Birth) timeWarning() *Warning {
	switch b.TimeStatus {
	case TimeApproximate:
		return &Warning{
			Code:     WarnBirthTimeApproximate,
			Severity: SeverityWarning,
			Message: "The time of birth is remembered or estimated rather than recorded. " +
				"The angles and the house cusps move about a degree every four minutes, so treat them as approximate too.",
			AffectedFields: []string{FieldAngles, FieldHouses, FieldPoints},
		}
	case TimeRectified:
		return &Warning{
			Code:     WarnBirthTimeRectified,
			Severity: SeverityInfo,
			Message: "The time of birth was rectified from events in the life rather than taken from a record. " +
				"The angles and houses are therefore the result of an interpretation, not an observation.",
			AffectedFields: []string{FieldAngles, FieldHouses},
		}
	case TimeUnknown:
		return &Warning{
			Code:     WarnBirthTimeUnknown,
			Severity: SeverityWarning,
			Message: "The time of birth is not known, so the time entered stands in for it. " +
				"The angles, the house cusps and every house placement are arbitrary; the signs of the slower planets are not.",
			AffectedFields: []string{FieldAngles, FieldHouses, FieldPoints, FieldMoonPhase},
		}
	}
	return nil
}
