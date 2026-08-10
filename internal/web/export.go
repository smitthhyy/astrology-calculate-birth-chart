package web

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"astronomyCalculator/internal/astro"
	"astronomyCalculator/internal/interp"
)

// The machine-readable form of a chart.
//
// One document holds everything the result page shows and the numbers behind
// it, so that another program never has to scrape the HTML or recompute
// anything to agree with what the reader was told. Six rules shape it:
//
//   - Calculated data and written interpretation live in separate sections. A
//     program analysing the chart, or an author replacing the readings with
//     better ones, should be able to take one without the other; mixing them
//     also invites a consumer to treat an editorial sentence as a fact.
//   - Every angle appears twice, once as a number and once as the parts a reader
//     would recognise. The number is authoritative; the formatted string is a
//     convenience, and both are given because deriving either from the other is
//     fiddly enough — truncated arcseconds, sign boundaries — that two consumers
//     would do it two ways.
//   - Anything not known is null rather than absent. The Ascendant has no rate
//     of motion here, so its speed is null and no aspect to it is marked
//     applying either way. A missing key cannot be told apart from a key nobody
//     thought to write.
//   - Every convention the chart depends on is stated: the zodiac, the house
//     system, which nodes, whose rulerships, what counts as an aspect and
//     between which points. Two programs can disagree about a chart while both
//     being right, and none of these choices is recoverable from a longitude.
//   - Points, houses and aspects cross-reference each other by the same
//     camelCase key, so the three tables join without string matching on
//     display names.
//   - Nothing that changes on its own is stored. The person's age is not here;
//     it is the birth date subtracted from whatever day it is being read on.
const (
	exportFormat = "birth-chart"
	// exportFormatVersion is the format's own semantic version. The major
	// component moves when a consumer written against the old shape would break;
	// the minor when fields are added that it can ignore.
	exportFormatVersion = "2.0.0"
	// exportVersion is the major component again, as an integer, for a consumer
	// that only wants to know which generation of the format it is holding.
	exportVersion = 2
	// schemaPath is where the JSON Schema for this document is served.
	schemaPath = "/birth-chart.schema.json"

	applicationName    = "Birth Chart Calculator"
	applicationVersion = "1.0.0"

	// interpretationLocale and interpretationVersion describe the written
	// readings, which are versioned apart from the calculation so that a chart
	// can be recast without rewriting the text and the text rewritten without
	// recasting the chart.
	interpretationLocale  = "en-AU"
	interpretationSystem  = "modern psychological astrology"
	interpretationVersion = "1.0.0"
)

// chartExport is the whole document.
type chartExport struct {
	Schema        string `json:"$schema"`
	Format        string `json:"format"`
	FormatVersion string `json:"formatVersion"`
	Version       int    `json:"version"`
	GeneratedAt   string `json:"generatedAt"`

	Generator generatorJSON `json:"generator"`
	// Settings are the astrological conventions the chart was cast under, as
	// against Generator, which is the software and data that cast it.
	Settings calculationSettingsJSON `json:"calculationSettings"`

	// RawInput is the form as typed, before any validation or normalisation —
	// locale-dependent dates, coordinates as strings, blanks where the picker
	// filled something in. Birth is the same information resolved.
	RawInput         rawInputJSON     `json:"rawInput"`
	Birth            birthJSON        `json:"birth"`
	BirthDataQuality birthQualityJSON `json:"birthDataQuality"`

	Chart chartDataJSON `json:"calculatedChart"`
	// Interpretations are the written readings, keyed to match the calculated
	// chart. A consumer wanting only the geometry can drop this whole object.
	Interpretations interpretationsJSON `json:"interpretations"`

	// Warnings are anything the reader was told about how the input was
	// interpreted — a doubtful time zone, a clock change, a house system that
	// had to be substituted, a placement that sits on a cusp. They belong with
	// the data, because each of them changes how far the chart can be trusted.
	Warnings []warningJSON `json:"warnings"`
}

// generatorJSON is the software and the data that produced the chart.
type generatorJSON struct {
	Application string `json:"application"`
	// ApplicationVersion is the program; CalculationEngineVersion is the part of
	// it that computes positions. The second is what matters for reproducing a
	// number, and it moves less often than the first.
	ApplicationVersion       string `json:"applicationVersion"`
	CalculationEngineVersion string `json:"calculationEngineVersion"`

	Ephemerides []ephemerisJSON `json:"ephemerides"`
	// EphemerisVersions is the same provenance flattened to name-to-version, for
	// a consumer that only wants to compare against its own tables.
	EphemerisVersions map[string]string `json:"ephemerisVersions"`
	TimeZoneDatabase  timeZoneDBJSON    `json:"timeZoneDatabase"`
	Formatting        formattingJSON    `json:"formatting"`
}

type ephemerisJSON struct {
	Key     string `json:"key"`
	Name    string `json:"name"`
	Covers  string `json:"covers"`
	Source  string `json:"source"`
	Version string `json:"version"`
}

// timeZoneDBDetail is how the zone database is described. The version is null
// because it cannot honestly be given: the Go standard library carries an
// embedded copy but does not publish its version, and on a host with its own
// zoneinfo files that copy is not the one used. Naming a version we cannot check
// would be worse than admitting we do not know it, since historical zone rules
// do get corrected and a consumer may need to know which correction applied.
type timeZoneDBJSON struct {
	Name    string  `json:"name"`
	Version *string `json:"version"`
	Source  string  `json:"source"`
}

// formattingJSON says how the display strings in this document were made, so
// that a consumer comparing them against its own is comparing like with like.
type formattingJSON struct {
	// DisplayPrecision is the smallest unit any formatted position carries.
	DisplayPrecision string `json:"displayPrecision"`
	// DisplayRounding is "truncate", which is the ephemeris convention and the
	// only safe choice: rounding 29°59'40" up would give 30°00', a degree that
	// belongs to the next sign.
	DisplayRounding string `json:"displayRounding"`
	// DegreeSeparators names the characters used, so a parser need not guess.
	DegreeSeparators string `json:"degreeSeparators"`
}

// calculationSettingsJSON states every astrological convention the chart
// depends on. All of it is fixed by this application rather than chosen per
// chart, but a consumer has no way to know that, and a future version may
// change any of it.
type calculationSettingsJSON struct {
	Zodiac string `json:"zodiac"`
	// HouseSystem is the machine-readable key; HouseSystemName is what it is
	// called. The key is what was actually used, which can differ from what was
	// asked for — see the warnings.
	HouseSystem     string `json:"houseSystem"`
	HouseSystemName string `json:"houseSystemName"`
	// NodeType is "true" or "mean". The two differ by up to about a degree and a
	// half, which is more than most orbs allow.
	NodeType string `json:"nodeType"`
	// LilithType says which Black Moon Lilith is given, when one is.
	LilithType string `json:"lilithType"`
	// RulershipSystem decides which planet a sign's ruler is, and so which body
	// the chart ruler is.
	RulershipSystem     string `json:"rulershipSystem"`
	ObserverPerspective string `json:"observerPerspective"`

	CoordinateReference coordinateReferenceJSON `json:"coordinateReference"`
	AspectRules         []aspectRuleJSON        `json:"aspectRules"`
	AspectCalculation   aspectCalculationJSON   `json:"aspectCalculation"`
	HousePlacementRules housePlacementJSON      `json:"housePlacementRules"`
}

type coordinateReferenceJSON struct {
	Frame    string `json:"frame"`
	Equinox  string `json:"equinox"`
	Ecliptic string `json:"ecliptic"`
}

type aspectRuleJSON struct {
	Name  string  `json:"name"`
	Key   string  `json:"key"`
	Glyph string  `json:"glyph"`
	Angle float64 `json:"angle"`
	Orb   float64 `json:"orb"`
	// LuminaryOrb is the orb allowed instead when the Sun or Moon is one of
	// the pair.
	LuminaryOrb float64 `json:"luminaryOrb"`
	Minor       bool    `json:"minor"`
}

// aspectCalculationJSON says exactly what was compared with what. Without it, a
// consumer finding aspects this document does not list cannot tell whether the
// two programs disagree about the geometry or about what counts as a point.
type aspectCalculationJSON struct {
	IncludedCategories   []string `json:"includedCategories"`
	IncludedPoints       []string `json:"includedPoints"`
	IncludeNodes         bool     `json:"includeNodes"`
	IncludeHouseCusps    bool     `json:"includeHouseCusps"`
	IncludeDerivedPoints bool     `json:"includeDerivedPoints"`
	IncludeMinorAspects  bool     `json:"includeMinorAspects"`
	// Note explains the omissions that are not obvious from the lists.
	Note string `json:"note"`
}

type housePlacementJSON struct {
	CuspConvention       string  `json:"cuspConvention"`
	CuspAllowanceDegrees float64 `json:"cuspAllowanceDegrees"`
}

// rawInputJSON is the form exactly as it was typed, so the document can be fed
// straight back in to reproduce itself.
type rawInputJSON struct {
	Name       string `json:"name"`
	Date       string `json:"date"`
	Time       string `json:"time"`
	Zone       string `json:"zone"`
	Location   string `json:"location"`
	Country    string `json:"country"`
	Latitude   string `json:"latitude"`
	Longitude  string `json:"longitude"`
	TimeStatus string `json:"timeStatus"`
	TimeSource string `json:"timeSource"`
}

// birthJSON is when and where, resolved.
type birthJSON struct {
	Name string `json:"name"`
	// Local is the instant with the birthplace's own offset; UTC is the same
	// instant. Between them they pin down the one input a reader cannot check
	// from the output.
	Local     string        `json:"local"`
	UTC       string        `json:"utc"`
	LocalDate string        `json:"localDate"`
	LocalTime string        `json:"localTime"`
	TimeZone  timeZoneJSON  `json:"timeZone"`
	Place     placeJSON     `json:"place"`
	JulianDay julianDayJSON `json:"julianDay"`
	Sidereal  siderealJSON  `json:"siderealTime"`
}

type timeZoneJSON struct {
	Name           string  `json:"name"`
	Abbreviation   *string `json:"abbreviation"`
	UTCOffset      string  `json:"utcOffset"`
	OffsetSeconds  int     `json:"utcOffsetSeconds"`
	DaylightSaving bool    `json:"daylightSaving"`
}

type placeJSON struct {
	Name      string  `json:"name"`
	Country   string  `json:"country"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	// LatitudeText and LongitudeText carry the hemisphere as a letter, which
	// is how coordinates are usually quoted in chart data and removes any
	// doubt about the sign convention.
	LatitudeText  string `json:"latitudeText"`
	LongitudeText string `json:"longitudeText"`
}

// birthQualityJSON says how much the birth data can be relied on. Nothing here
// changes a single computed number; all of it changes what those numbers are
// worth. A chart whose time was guessed has an Ascendant, and it means nothing.
type birthQualityJSON struct {
	// TimeStatus is "recorded", "approximate", "rectified", "unknown" or
	// "unstated". The last is the default and is not a claim of accuracy.
	TimeStatus string `json:"timeStatus"`
	// TimeStatusLabel is the same thing as a phrase, for a report.
	TimeStatusLabel string `json:"timeStatusLabel"`
	// Source is whatever the reader said the time came from, or null.
	Source *string `json:"source"`
	// TimeAccuracySeconds is how close to the truth the time can be taken to be,
	// or null when nothing is known about that. A recorded time gets sixty
	// seconds, which is the granularity of the form rather than a judgement
	// about the record.
	TimeAccuracySeconds *int `json:"timeAccuracySeconds"`
	// Rectified is stated separately because a consumer may want to exclude
	// rectified charts from statistical use without caring about the rest.
	Rectified bool `json:"rectified"`
	// LocationSource says where the coordinates came from.
	LocationSource string `json:"locationSource"`
	// LocationAccuracyMetres is null: a gazetteer record is a named point, not a
	// bounded area, so no radius can be derived from it. A city's own extent —
	// which is what the uncertainty really is — is not in the data.
	LocationAccuracyMetres *float64 `json:"locationAccuracyMetres"`
}

type julianDayJSON struct {
	UniversalTime   float64 `json:"universalTime"`
	TerrestrialTime float64 `json:"terrestrialTime"`
}

// siderealJSON is the local apparent sidereal time, which is also the right
// ascension of the meridian. It is the one number another program needs to
// rebuild these cusps under a different house system.
type siderealJSON struct {
	LocalApparentDegrees float64 `json:"localApparentDegrees"`
	LocalApparentHours   string  `json:"localApparentHours"`
	// ObliquityDegrees is the true obliquity of the ecliptic on the day, which
	// everything converting between ecliptic and equatorial coordinates needs.
	ObliquityDegrees float64 `json:"obliquityDegrees"`
}

// chartDataJSON is the chart proper: calculated values only.
type chartDataJSON struct {
	HouseSystem string `json:"houseSystem"`
	// Sect is "day" when the Sun stood above the horizon at birth, and null when
	// it could not be determined.
	Sect *string `json:"sect"`
	// Angles are the four cardinal points as plain ecliptic longitudes. Their
	// full detail is in Points, under the keys ascendant, descendant,
	// midheaven and imumCoeli.
	Angles anglesJSON  `json:"angles"`
	Points []pointJSON `json:"points"`
	// DerivedPoints are computed from the angles and the luminaries rather than
	// read from an ephemeris. They are kept apart because each needs its own
	// formula stated beside it, and because none of them is a house occupant.
	DerivedPoints []pointJSON      `json:"derivedPoints"`
	Houses        []houseJSON      `json:"houses"`
	Aspects       []aspectJSON     `json:"aspects"`
	MoonPhase     *moonPhaseJSON   `json:"moonPhase"`
	ChartRuler    *chartRulerJSON  `json:"chartRuler"`
	Distribution  distributionJSON `json:"distribution"`
}

type anglesJSON struct {
	Ascendant  float64 `json:"ascendant"`
	Descendant float64 `json:"descendant"`
	Midheaven  float64 `json:"midheaven"`
	ImumCoeli  float64 `json:"imumCoeli"`
}

// pointJSON is one body, angle, node or derived point — calculation only.
type pointJSON struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Glyph    string `json:"glyph"`
	Category string `json:"category"`

	// LongitudeDegrees is the authoritative position. Everything else in this
	// object is either derived from it or a different way of writing it.
	LongitudeDegrees float64         `json:"longitudeDegrees"`
	Position         positionJSON    `json:"position"`
	Coordinates      coordinatesJSON `json:"coordinates"`
	Decan            int             `json:"decan"`

	Sign  signJSON `json:"sign"`
	House int      `json:"house"`

	// SpeedPerDay is null for the four angles and the derived points: their
	// motion comes from the Earth's rotation or from the chart's own arithmetic
	// rather than from the ephemeris, and reporting zero would read as
	// "stationary".
	SpeedPerDay *float64 `json:"speedPerDay"`
	Retrograde  bool     `json:"retrograde"`

	// Derivation is present only for a point computed from the chart itself, and
	// says by which of the competing formulae. There is more than one Part of
	// Fortune in circulation and a bare longitude cannot be compared.
	Derivation *derivationJSON `json:"derivation,omitempty"`
}

// positionJSON is the same longitude as the sign and the degrees within it,
// broken out so nothing has to parse a formatted string to recover a number.
type positionJSON struct {
	Sign    string `json:"sign"`
	SignKey string `json:"signKey"`
	Degree  int    `json:"degree"`
	Minute  int    `json:"minute"`
	// Second keeps its fraction, so the three parts add back up to
	// DegreeInSignDegrees exactly.
	Second              float64 `json:"second"`
	DegreeInSignDegrees float64 `json:"degreeInSignDegrees"`
	// Formatted is for display only, truncated to the arcsecond.
	Formatted string `json:"formatted"`
}

// coordinatesJSON gives the position in both systems. Declination cannot be
// recovered from ecliptic longitude alone, and it is what parallels and
// contra-parallels are read from.
type coordinatesJSON struct {
	EclipticLongitudeDegrees float64 `json:"eclipticLongitudeDegrees"`
	EclipticLatitudeDegrees  float64 `json:"eclipticLatitudeDegrees"`
	RightAscensionDegrees    float64 `json:"rightAscensionDegrees"`
	DeclinationDegrees       float64 `json:"declinationDegrees"`
}

type derivationJSON struct {
	// FormulaType names the variant — "day" or "night" for a lot, "mean" or
	// "true" for a lunar point, "western" or "eastern" for the prime vertical.
	FormulaType string `json:"formulaType"`
	Formula     string `json:"formula"`
}

type signJSON struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Glyph    string `json:"glyph"`
	Number   int    `json:"number"`
	Element  string `json:"element"`
	Modality string `json:"modality"`
	Polarity string `json:"polarity"`
	// Ruler is the ruler under the scheme this chart was cast in, which
	// calculationSettings.rulershipSystem names. Rulers carries both, so a
	// consumer reading traditionally can take the other without recasting.
	Ruler  string     `json:"ruler"`
	Rulers rulersJSON `json:"rulers"`
}

type rulersJSON struct {
	Traditional string `json:"traditional"`
	Modern      string `json:"modern"`
}

type houseJSON struct {
	Number int      `json:"number"`
	Cusp   cuspJSON `json:"cusp"`
	Sign   signJSON `json:"sign"`
	// Ruler is the ruler of the sign on the cusp — the house's own significator.
	Ruler string `json:"ruler"`
	// Occupants are the keys of the bodies falling in the house. The angles are
	// left out: they are the cusps themselves, so listing them would be noise.
	// So are the derived points, which are not bodies.
	Occupants []string `json:"occupants"`
}

type cuspJSON struct {
	LongitudeDegrees float64      `json:"longitudeDegrees"`
	Position         positionJSON `json:"position"`
}

type aspectJSON struct {
	// ID is a stable identifier for the pairing, e.g. "sun-square-saturn". It is
	// what the interpretations are keyed by.
	ID        string `json:"id"`
	Between   string `json:"between"`
	A         string `json:"a"`
	B         string `json:"b"`
	Aspect    string `json:"aspect"`
	AspectKey string `json:"aspectKey"`
	Glyph     string `json:"glyph"`
	Nature    string `json:"nature"`
	Minor     bool   `json:"minor"`
	// ExactAngleDegrees is the angle the aspect is named for;
	// SeparationDegrees the angle the pair actually stands at; OrbDegrees the
	// difference; OrbAllowedDegrees the widest it could have been and still
	// counted, which is what makes a marginal aspect recognisable.
	ExactAngleDegrees float64 `json:"exactAngleDegrees"`
	SeparationDegrees float64 `json:"separationDegrees"`
	OrbDegrees        float64 `json:"orbDegrees"`
	OrbFormatted      string  `json:"orbFormatted"`
	OrbAllowedDegrees float64 `json:"orbAllowedDegrees"`
	// Applying is null when the pair includes an angle, whose rate of motion the
	// chart does not hold. Null is not false: one means the question does not
	// apply, the other that the pair is separating.
	Applying *bool `json:"applying"`
}

type moonPhaseJSON struct {
	// Name is the almanac name, which always agrees with Illumination;
	// LunationPhase is the eighth of the cycle that natal astrology reads.
	Name          string `json:"name"`
	LunationPhase string `json:"lunationPhase"`
	// Angle is the Moon's elongation from the Sun, zero at the new moon.
	Angle        float64 `json:"angle"`
	Illumination float64 `json:"illumination"`
}

type chartRulerJSON struct {
	AscendantSign string `json:"ascendantSign"`
	Ruler         string `json:"ruler"`
	RulerKey      string `json:"rulerKey"`
	// Position and House are null when the ruling body is not one this chart
	// computes, which cannot happen under either rulership scheme but is not
	// guaranteed by the types.
	Position *string `json:"position"`
	House    *int    `json:"house"`
}

// distributionJSON is the balance of the chart: the counts astrologers read at
// a glance before looking at anything in detail.
type distributionJSON struct {
	// Basis names what was counted, because there is no single convention and
	// a bare count of four in Fire is meaningless without it.
	Basis string `json:"basis"`
	// CountedPoints is the same thing as a list, for a program.
	CountedPoints []string       `json:"countedPoints"`
	Elements      map[string]int `json:"elements"`
	Modalities    map[string]int `json:"modalities"`
	Polarities    map[string]int `json:"polarities"`
	Hemispheres   map[string]int `json:"hemispheres"`
	Quadrants     map[string]int `json:"quadrants"`
	// Retrograde lists the keys of the bodies moving backwards at birth.
	Retrograde []string `json:"retrograde"`
}

// interpretationsJSON is the written text, keyed to the calculated chart and
// versioned apart from it.
type interpretationsJSON struct {
	Locale                string `json:"locale"`
	System                string `json:"system"`
	InterpretationVersion string `json:"interpretationVersion"`
	GeneratedBy           string `json:"generatedBy"`
	// Points is keyed by point key, Houses by house number as a string, and
	// Aspects by the aspect's id.
	Points  map[string]pointReadingJSON  `json:"points"`
	Houses  map[string]houseReadingJSON  `json:"houses"`
	Aspects map[string]aspectReadingJSON `json:"aspects"`
}

type pointReadingJSON struct {
	// Defines is what the body signifies at all; Definition what the sign does
	// to that; ShowsUp how it appears in a life. Any of them may be empty for a
	// point that has only some of the three written.
	Defines    string `json:"defines,omitempty"`
	Definition string `json:"definition,omitempty"`
	ShowsUp    string `json:"showsUp,omitempty"`
}

type houseReadingJSON struct {
	Governs string `json:"governs"`
	ShowsUp string `json:"showsUp,omitempty"`
}

type aspectReadingJSON struct {
	ShowsUp string `json:"showsUp"`
}

type warningJSON struct {
	Code           string   `json:"code"`
	Severity       string   `json:"severity"`
	Message        string   `json:"message"`
	AffectedFields []string `json:"affectedFields"`
}

// distributionBodies are the ten planets. The angles are excluded because they
// are not bodies and would double-count the quadrants they define; the nodes
// because they are always exactly opposite each other and would cancel out.
var distributionBodies = []astro.Body{
	astro.Sun, astro.Moon, astro.Mercury, astro.Venus, astro.Mars,
	astro.Jupiter, astro.Saturn, astro.Uranus, astro.Neptune, astro.Pluto,
}

const distributionBasis = "Counted over the ten planets, the Sun and Moon included; " +
	"the angles, the lunar nodes and the derived points are not counted. " +
	"Northern and southern are below and above the horizon (houses 1–6 and 7–12); " +
	"eastern and western are east and west of the meridian (houses 10–3 and 4–9)."

const aspectCalculationNote = "The Descendant and the Imum Coeli are omitted because every aspect to them " +
	"would duplicate one to the opposite angle. The lunar nodes are omitted for the same reason as each other, " +
	"and the derived points because they have no independent motion. " +
	"Applying and separating are not determined for a pair including an angle, whose rate of motion is not held."

// buildExport assembles the document from the form as typed and the chart it
// produced.
func buildExport(in Input, c *astro.Chart) chartExport {
	return chartExport{
		Schema:        schemaPath,
		Format:        exportFormat,
		FormatVersion: exportFormatVersion,
		Version:       exportVersion,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		Generator:     exportGenerator(),
		Settings:      exportSettings(c),
		RawInput: rawInputJSON{
			Name: in.Name, Date: in.Date, Time: in.Time, Zone: in.Zone,
			Location: in.Location, Country: in.Country,
			Latitude: in.Latitude, Longitude: in.Longitude,
			TimeStatus: in.TimeStatus, TimeSource: in.TimeSource,
		},
		Birth:            exportBirth(c),
		BirthDataQuality: exportBirthQuality(in, c),
		Chart:            exportChart(c),
		Interpretations:  exportInterpretations(c),
		Warnings:         exportWarnings(c),
	}
}

func exportGenerator() generatorJSON {
	sources := astro.Ephemerides()
	list := make([]ephemerisJSON, 0, len(sources))
	versions := make(map[string]string, len(sources))
	for _, e := range sources {
		list = append(list, ephemerisJSON{
			Key: e.Key, Name: e.Name, Covers: e.Covers,
			Source: e.Source, Version: e.Version,
		})
		versions[e.Name] = e.Version
	}

	return generatorJSON{
		Application:              applicationName,
		ApplicationVersion:       applicationVersion,
		CalculationEngineVersion: astro.EngineVersion,
		Ephemerides:              list,
		EphemerisVersions:        versions,
		TimeZoneDatabase: timeZoneDBJSON{
			Name:    "IANA TZDB",
			Version: nil,
			Source: "the Go standard library's embedded copy (time/tzdata), " +
				"or the host's own zoneinfo files where it has them. Neither publishes its version at run time, " +
				"so it is given as null rather than guessed at.",
		},
		Formatting: formattingJSON{
			DisplayPrecision: "arcsecond",
			DisplayRounding:  "truncate",
			DegreeSeparators: `degrees °, arcminutes ', arcseconds "`,
		},
	}
}

func exportSettings(c *astro.Chart) calculationSettingsJSON {
	rules := make([]aspectRuleJSON, 0, len(astro.AspectSet(c.Options)))
	for _, k := range astro.AspectSet(c.Options) {
		rules = append(rules, aspectRuleJSON{
			Name: k.Name, Key: keyOf(k.Name), Glyph: k.Glyph, Angle: k.Angle,
			Orb: k.Orb, LuminaryOrb: k.LuminaryOrb, Minor: k.Minor,
		})
	}

	categories := map[string]bool{}
	points := make([]string, 0, len(astro.AspectBodies))
	var includesNodes bool
	for _, b := range astro.AspectBodies {
		points = append(points, b.Key())
		categories[b.Category()] = true
		if b == astro.NorthNode || b == astro.SouthNode {
			includesNodes = true
		}
	}

	return calculationSettingsJSON{
		Zodiac:              astro.Zodiac,
		HouseSystem:         keyOf(string(c.Houses.System)),
		HouseSystemName:     string(c.Houses.System),
		NodeType:            astro.NodeType,
		LilithType:          lilithType(c),
		RulershipSystem:     string(astro.Rulership),
		ObserverPerspective: astro.ObserverPerspective,
		CoordinateReference: coordinateReferenceJSON{
			Frame:    astro.CoordinateFrame,
			Equinox:  astro.CoordinateEquinox,
			Ecliptic: astro.CoordinateEcliptic,
		},
		AspectRules: rules,
		AspectCalculation: aspectCalculationJSON{
			IncludedCategories:   sortedKeys(categories),
			IncludedPoints:       points,
			IncludeNodes:         includesNodes,
			IncludeHouseCusps:    false,
			IncludeDerivedPoints: false,
			IncludeMinorAspects:  c.Options.MinorAspects,
			Note:                 aspectCalculationNote,
		},
		HousePlacementRules: housePlacementJSON{
			CuspConvention:       astro.CuspConvention,
			CuspAllowanceDegrees: astro.CuspAllowanceDegrees,
		},
	}
}

// lilithType reports which Black Moon Lilith the chart carries, or "none" when
// the derived points could not be computed.
func lilithType(c *astro.Chart) string {
	if d, ok := c.Derivation(astro.BlackMoonLilith); ok {
		if _, present := c.Placement(astro.BlackMoonLilith); present {
			return d.Method
		}
	}
	return "none"
}

func exportBirth(c *astro.Chart) birthJSON {
	abbrev, offset := c.Local.Zone()
	// A zone with no name of its own reports its offset as the abbreviation,
	// which would only repeat utcOffset.
	var abbreviation *string
	if !strings.HasPrefix(abbrev, "+") && !strings.HasPrefix(abbrev, "-") && abbrev != "" {
		abbreviation = &abbrev
	}

	return birthJSON{
		Name:      c.Birth.Name,
		Local:     c.Local.Format(time.RFC3339),
		UTC:       c.UTC.Format(time.RFC3339),
		LocalDate: c.Local.Format("2006-01-02"),
		LocalTime: c.Local.Format("15:04"),
		TimeZone: timeZoneJSON{
			Name:           c.Birth.ZoneName,
			Abbreviation:   abbreviation,
			UTCOffset:      offsetLabel(offset),
			OffsetSeconds:  offset,
			DaylightSaving: c.Local.IsDST(),
		},
		Place: placeJSON{
			Name:          c.Birth.Place,
			Country:       c.Birth.Country,
			Latitude:      c.Birth.Latitude,
			Longitude:     c.Birth.Longitude,
			LatitudeText:  fmt.Sprintf("%.4f°%s", abs(c.Birth.Latitude), northSouth(c.Birth.Latitude)),
			LongitudeText: fmt.Sprintf("%.4f°%s", abs(c.Birth.Longitude), eastWest(c.Birth.Longitude)),
		},
		JulianDay: julianDayJSON{
			UniversalTime:   c.JulianDay,
			TerrestrialTime: c.JDE,
		},
		Sidereal: siderealJSON{
			LocalApparentDegrees: c.Houses.RAMC,
			LocalApparentHours:   siderealHours(c.Houses.RAMC),
			ObliquityDegrees:     c.Houses.Obliquity,
		},
	}
}

func exportBirthQuality(in Input, c *astro.Chart) birthQualityJSON {
	status := c.Birth.TimeStatus

	out := birthQualityJSON{
		TimeStatus:      string(status),
		TimeStatusLabel: status.Label(),
		Rectified:       status.Rectified(),
		LocationSource:  locationSource(in),
		// See the field's own comment: a named point carries no radius.
		LocationAccuracyMetres: nil,
	}
	if source := strings.TrimSpace(c.Birth.TimeSource); source != "" {
		out.Source = &source
	}
	if seconds, ok := status.AccuracySeconds(); ok {
		out.TimeAccuracySeconds = &seconds
	}
	return out
}

// locationSource says where the coordinates came from, which is the only thing
// that can honestly be said about their accuracy.
func locationSource(in Input) string {
	if in.Latitude != "" && in.Longitude != "" {
		return "coordinates supplied with the request, either typed by hand or filled in by the place picker"
	}
	return "the embedded gazetteer, which gives one named point per place rather than an area"
}

func exportChart(c *astro.Chart) chartDataJSON {
	out := chartDataJSON{
		HouseSystem: string(c.Houses.System),
		Angles: anglesJSON{
			Ascendant:  c.Houses.Ascendant,
			Descendant: c.Houses.Descendant(),
			Midheaven:  c.Houses.Midheaven,
			ImumCoeli:  c.Houses.ImumCoeli(),
		},
		Points:        exportPoints(c),
		DerivedPoints: exportDerivedPoints(c),
		Houses:        exportHouses(c),
		Aspects:       exportAspects(c),
		Distribution:  exportDistribution(c),
	}
	if sect, ok := c.Sect(); ok {
		out.Sect = &sect
	}
	if phase, ok := c.MoonPhase(); ok {
		out.MoonPhase = &moonPhaseJSON{
			Name:          phase.Name,
			LunationPhase: phase.Lunation,
			Angle:         phase.Angle,
			Illumination:  phase.Illumination,
		}
	}
	out.ChartRuler = exportChartRuler(c)
	return out
}

func exportPoints(c *astro.Chart) []pointJSON {
	// The same order the page presents them in: the bodies with a full reading
	// first, then the secondary points.
	order := append(append([]astro.Body{}, astro.TableBodies...), astro.PointBodies...)

	points := make([]pointJSON, 0, len(order))
	for _, body := range order {
		p, ok := c.Placement(body)
		if !ok {
			continue
		}
		points = append(points, exportPoint(body, p))
	}
	return points
}

func exportDerivedPoints(c *astro.Chart) []pointJSON {
	points := make([]pointJSON, 0, len(c.Derived))
	for _, p := range c.Derived {
		point := exportPoint(p.Body, p)
		if d, ok := c.Derivation(p.Body); ok {
			point.Derivation = &derivationJSON{FormulaType: d.Method, Formula: d.Formula}
		}
		points = append(points, point)
	}
	return points
}

func exportPoint(body astro.Body, p astro.Placement) pointJSON {
	point := pointJSON{
		Key:              body.Key(),
		Name:             body.Name(),
		Glyph:            body.Glyph(),
		Category:         body.Category(),
		LongitudeDegrees: p.Longitude,
		Position:         positionOf(p.Longitude),
		Coordinates: coordinatesJSON{
			EclipticLongitudeDegrees: p.Longitude,
			EclipticLatitudeDegrees:  p.Latitude,
			RightAscensionDegrees:    p.RightAscension,
			DeclinationDegrees:       p.Declination,
		},
		Decan:      astro.DecanOf(p.Longitude),
		Sign:       signOf(p.Sign),
		House:      p.House,
		Retrograde: p.Retrograde,
	}
	// Only a body read from the ephemeris has a rate of motion. For the angles
	// and the derived points the honest answer is that there is none to give.
	if !body.IsAngle() && !body.IsDerived() {
		speed := p.Speed
		point.SpeedPerDay = &speed
	}
	return point
}

// positionOf breaks a longitude into the parts a reader recognises, keeping the
// numbers alongside the string so nothing has to parse it back.
func positionOf(lon float64) positionJSON {
	inSign := astro.DegreeInSign(lon)
	d, m, s := astro.SplitDMS(inSign)
	sign := astro.SignOf(lon)

	return positionJSON{
		Sign:                sign.String(),
		SignKey:             sign.Key(),
		Degree:              d,
		Minute:              m,
		Second:              s,
		DegreeInSignDegrees: inSign,
		Formatted:           astro.FormatPositionSeconds(lon),
	}
}

func exportHouses(c *astro.Chart) []houseJSON {
	houses := make([]houseJSON, 0, len(c.Houses12))
	for _, h := range c.Houses12 {
		occupants := make([]string, 0, len(h.Occupants))
		for _, b := range h.Occupants {
			occupants = append(occupants, b.Key())
		}
		houses = append(houses, houseJSON{
			Number: h.Number,
			Cusp: cuspJSON{
				LongitudeDegrees: h.Cusp,
				Position:         positionOf(h.Cusp),
			},
			Sign:      signOf(h.Sign),
			Ruler:     h.Sign.Ruler(),
			Occupants: occupants,
		})
	}
	return houses
}

func exportAspects(c *astro.Chart) []aspectJSON {
	aspects := make([]aspectJSON, 0, len(c.Aspects))
	for _, a := range c.Aspects {
		entry := aspectJSON{
			ID:                aspectID(a),
			Between:           a.A.Name() + " – " + a.B.Name(),
			A:                 a.A.Key(),
			B:                 a.B.Key(),
			Aspect:            a.Name,
			AspectKey:         keyOf(a.Name),
			Glyph:             a.Glyph,
			Nature:            a.Nature(),
			Minor:             a.Minor(),
			ExactAngleDegrees: a.Angle,
			SeparationDegrees: a.Separation,
			OrbDegrees:        a.Orb,
			OrbFormatted:      astro.FormatDMSSeconds(a.Orb),
			OrbAllowedDegrees: c.OrbAllowed(a),
		}
		if applying, known := c.Applying(a); known {
			entry.Applying = &applying
		}
		aspects = append(aspects, entry)
	}
	return aspects
}

// aspectID names a pairing in a way that is stable across runs and readable in a
// log: "sun-square-saturn".
func aspectID(a astro.Aspect) string {
	return strings.Join([]string{keyOf(a.A.Key()), keyOf(a.Name), keyOf(a.B.Key())}, "-")
}

// keyOf lower-cases a display name into a machine-readable key, so that
// "Whole Sign" becomes "whole-sign" and "Square" becomes "square".
func keyOf(name string) string {
	var b strings.Builder
	pendingDash := false
	for _, r := range strings.ToLower(name) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			if pendingDash && b.Len() > 0 {
				b.WriteByte('-')
			}
			pendingDash = false
			b.WriteRune(r)
		default:
			pendingDash = true
		}
	}
	return b.String()
}

func exportChartRuler(c *astro.Chart) *chartRulerJSON {
	asc, ok := c.Placement(astro.Ascendant)
	if !ok {
		return nil
	}
	ruler := chartRulerJSON{AscendantSign: asc.Sign.String(), Ruler: asc.Sign.Ruler()}

	body, found := astro.BodyByName(ruler.Ruler)
	if !found {
		return &ruler
	}
	ruler.RulerKey = body.Key()
	if p, ok := c.Placement(body); ok {
		position := astro.FormatPositionSeconds(p.Longitude)
		house := p.House
		ruler.Position = &position
		ruler.House = &house
	}
	return &ruler
}

func exportDistribution(c *astro.Chart) distributionJSON {
	counted := make([]string, 0, len(distributionBodies))
	for _, body := range distributionBodies {
		counted = append(counted, body.Key())
	}

	d := distributionJSON{
		Basis:         distributionBasis,
		CountedPoints: counted,
		Elements:      map[string]int{"Fire": 0, "Earth": 0, "Air": 0, "Water": 0},
		Modalities:    map[string]int{"Cardinal": 0, "Fixed": 0, "Mutable": 0},
		Polarities:    map[string]int{"Positive": 0, "Negative": 0},
		Hemispheres:   map[string]int{"eastern": 0, "western": 0, "northern": 0, "southern": 0},
		Quadrants:     map[string]int{"first": 0, "second": 0, "third": 0, "fourth": 0},
		Retrograde:    []string{},
	}
	quadrantNames := [4]string{"first", "second", "third", "fourth"}

	for _, body := range distributionBodies {
		p, ok := c.Placement(body)
		if !ok {
			continue
		}
		d.Elements[p.Sign.Element()]++
		d.Modalities[p.Sign.Modality()]++
		d.Polarities[p.Sign.Polarity()]++

		// Houses 1–6 lie below the horizon and 7–12 above it; houses 10–12 and
		// 1–3 lie east of the meridian and 4–9 west of it.
		if p.House <= 6 {
			d.Hemispheres["northern"]++
		} else {
			d.Hemispheres["southern"]++
		}
		if p.House <= 3 || p.House >= 10 {
			d.Hemispheres["eastern"]++
		} else {
			d.Hemispheres["western"]++
		}
		d.Quadrants[quadrantNames[(p.House-1)/3]]++

		if p.Retrograde {
			d.Retrograde = append(d.Retrograde, body.Key())
		}
	}
	return d
}

// exportInterpretations collects the written text, keyed so that it joins back
// onto the calculated chart without any string matching on display names.
func exportInterpretations(c *astro.Chart) interpretationsJSON {
	out := interpretationsJSON{
		Locale:                interpretationLocale,
		System:                interpretationSystem,
		InterpretationVersion: interpretationVersion,
		GeneratedBy:           applicationName,
		Points:                map[string]pointReadingJSON{},
		Houses:                map[string]houseReadingJSON{},
		Aspects:               map[string]aspectReadingJSON{},
	}

	for _, p := range c.Placements {
		if reading, ok := readingFor(p.Body, p.Sign); ok {
			out.Points[p.Body.Key()] = reading
		}
	}
	for _, h := range c.Houses12 {
		reading := houseReadingJSON{Governs: h.Meaning}
		if showsUp, ok := interp.HouseReading(h.Number, h.Sign.Key()); ok {
			reading.ShowsUp = showsUp
		}
		out.Houses[strconv.Itoa(h.Number)] = reading
	}
	for _, a := range c.Aspects {
		if showsUp, ok := interp.AspectReading(a.A.Key(), a.B.Key(), a.Name); ok {
			out.Aspects[aspectID(a)] = aspectReadingJSON{ShowsUp: showsUp}
		}
	}
	return out
}

// readingFor collects whatever written text exists for a placement. The planets
// and the two horizon angles have a definition and a manifestation; the
// secondary points have only the latter; the derived points have neither.
func readingFor(body astro.Body, sign astro.Sign) (pointReadingJSON, bool) {
	if b, r, ok := interp.Lookup(body.Key(), sign.Key()); ok {
		return pointReadingJSON{Defines: b.Defines, Definition: r.Definition, ShowsUp: r.ShowsUp}, true
	}
	defines, hasDefines := interp.PointDefines(body.Key())
	showsUp, hasReading := interp.PointReading(body.Key(), sign.Key())
	if !hasDefines && !hasReading {
		return pointReadingJSON{}, false
	}
	return pointReadingJSON{Defines: defines, ShowsUp: showsUp}, true
}

func exportWarnings(c *astro.Chart) []warningJSON {
	// An absent array reads as a failure to look, so an empty one is written.
	out := make([]warningJSON, 0, len(c.Warnings))
	for _, w := range c.Warnings {
		fields := w.AffectedFields
		if fields == nil {
			fields = []string{}
		}
		out = append(out, warningJSON{
			Code: w.Code, Severity: w.Severity, Message: w.Message, AffectedFields: fields,
		})
	}
	return out
}

func signOf(s astro.Sign) signJSON {
	return signJSON{
		Key:      s.Key(),
		Name:     s.String(),
		Glyph:    s.Glyph(),
		Number:   s.Number(),
		Element:  s.Element(),
		Modality: s.Modality(),
		Polarity: s.Polarity(),
		Ruler:    s.Ruler(),
		Rulers: rulersJSON{
			Traditional: s.TraditionalRuler(),
			Modern:      s.ModernRuler(),
		},
	}
}

// sortedKeys returns the set's members in a stable order, so that two exports of
// the same chart are byte-identical.
func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// offsetLabel renders a zone offset in seconds as e.g. `+10:00`.
func offsetLabel(seconds int) string {
	sign := "+"
	if seconds < 0 {
		sign, seconds = "-", -seconds
	}
	return fmt.Sprintf("%s%02d:%02d", sign, seconds/3600, (seconds%3600)/60)
}

// siderealHours renders a sidereal time given in degrees as clock time, which
// is how ephemerides quote it.
func siderealHours(deg float64) string {
	total := int(deg / 15 * 3600)
	return fmt.Sprintf("%02dh%02dm%02ds", total/3600, (total/60)%60, total%60)
}

// exportFilename turns a first name into a downloadable file name.
func exportFilename(name string) string {
	slug := keyOf(name)
	if len(slug) > 40 {
		slug = strings.Trim(slug[:40], "-")
	}
	if slug == "" {
		return "birth-chart.json"
	}
	return slug + "-birth-chart.json"
}
