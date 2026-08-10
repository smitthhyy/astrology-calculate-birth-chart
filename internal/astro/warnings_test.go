package astro

import (
	"math"
	"strings"
	"testing"
	"time"
)

// Every warning a chart produces has to be actionable without reading the
// English: a code to switch on, a severity to filter by, and the fields it casts
// doubt on.
func TestWarningsAreStructured(t *testing.T) {
	// Tromsø, well inside the arctic circle, so Placidus has no solution and the
	// chart has to say so.
	chart, err := Compute(Birth{
		Year: 1985, Month: time.March, Day: 3, Hour: 6, Minute: 15,
		Zone: mustZone(t, "Europe/Oslo"), ZoneName: "Europe/Oslo",
		Place: "Tromsø", Country: "Norway",
		Latitude: 69.6492, Longitude: 18.9553,
	})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	var found bool
	for _, w := range chart.Warnings {
		if w.Code == "" {
			t.Errorf("a warning has no code: %+v", w)
		}
		if w.Severity != SeverityWarning && w.Severity != SeverityInfo {
			t.Errorf("%s: severity %q is neither warning nor info", w.Code, w.Severity)
		}
		if !strings.HasSuffix(strings.TrimSpace(w.Message), ".") {
			t.Errorf("%s: the message is not a sentence: %q", w.Code, w.Message)
		}
		if len(w.AffectedFields) == 0 {
			t.Errorf("%s: no affected fields", w.Code)
		}
		if w.Code == WarnPlacidusUnavailable {
			found = true
			if w.Severity != SeverityWarning {
				t.Errorf("a substituted house system is a warning, not %q", w.Severity)
			}
		}
	}
	if !found {
		t.Errorf("no %s warning for a chart at 69.6°N: %+v", WarnPlacidusUnavailable, chart.Warnings)
	}
	if chart.Houses.System != WholeSign {
		t.Errorf("house system = %s, want %s", chart.Houses.System, WholeSign)
	}
}

// A clock change is a warning rather than a note, because the chart it produced
// could be an hour wrong.
func TestClockChangeWarningReachesTheChart(t *testing.T) {
	// 01:30 on 2023-11-05 happened twice in New York.
	chart, err := Compute(Birth{
		Year: 2023, Month: time.November, Day: 5, Hour: 1, Minute: 30,
		Zone: mustZone(t, "America/New_York"), ZoneName: "America/New_York",
		Latitude: 40.7, Longitude: -74,
	})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	for _, w := range chart.Warnings {
		if w.Code == WarnClockChangeRepeated {
			if w.Severity != SeverityWarning {
				t.Errorf("severity = %q, want %q", w.Severity, SeverityWarning)
			}
			return
		}
	}
	t.Errorf("no %s warning: %+v", WarnClockChangeRepeated, chart.Warnings)
}

// A near-cusp note has to name the house the point would move into, and that has
// to be a house it could actually move into — the one on the other side of the
// cusp it is sitting beside.
func TestNearCuspNotesNameTheNeighbouringHouse(t *testing.T) {
	chart := goldenChart(t)

	for _, w := range chart.Warnings {
		if w.Code != WarnPointNearCusp {
			continue
		}
		if w.Severity != SeverityInfo {
			t.Errorf("a correct placement beside a cusp is info, not %q", w.Severity)
		}
	}

	// The arithmetic behind the note, checked directly: a point past the cusp
	// came from the house before, and a point short of it is already there.
	if got, want := otherSideOf(5, 0.4), 4; got != want {
		t.Errorf("otherSideOf(5, +0.4) = %d, want %d", got, want)
	}
	if got, want := otherSideOf(5, -0.4), 5; got != want {
		t.Errorf("otherSideOf(5, -0.4) = %d, want %d", got, want)
	}
	if got, want := otherSideOf(1, 0.4), 12; got != want {
		t.Errorf("otherSideOf(1, +0.4) = %d, want %d", got, want)
	}
}

// Every aspect flagged as marginal has to actually be within a degree of the
// limit it was admitted under, and nothing else may be flagged.
func TestMarginalAspectNotesMatchTheOrbs(t *testing.T) {
	chart := goldenChart(t)

	flagged := map[string]bool{}
	for _, w := range chart.Warnings {
		if w.Code == WarnAspectNearOrbLimit {
			flagged[w.Message] = true
		}
	}

	for _, a := range chart.Aspects {
		allowed := chart.OrbAllowed(a)
		if allowed == 0 {
			t.Errorf("%s %s %s: no orb rule found", a.A.Name(), a.Name, a.B.Name())
			continue
		}
		if a.Orb > allowed {
			t.Errorf("%s %s %s: orb %.4f exceeds the %.1f allowed", a.A.Name(), a.Name, a.B.Name(), a.Orb, allowed)
		}
		marginal := allowed-a.Orb <= orbProximity
		var noted bool
		for msg := range flagged {
			if strings.Contains(msg, a.Name) && strings.Contains(msg, a.A.Name()) && strings.Contains(msg, a.B.Name()) {
				noted = true
			}
		}
		if marginal != noted {
			t.Errorf("%s %s %s: orb %.4f of %.1f, marginal = %v but noted = %v",
				a.A.Name(), a.Name, a.B.Name(), a.Orb, allowed, marginal, noted)
		}
	}
}

func TestOrdinal(t *testing.T) {
	cases := map[int]string{
		1: "1st", 2: "2nd", 3: "3rd", 4: "4th", 11: "11th", 12: "12th", 13: "13th",
		21: "21st", 22: "22nd", 23: "23rd", 100: "100th", 101: "101st", 111: "111th",
	}
	for n, want := range cases {
		if got := ordinal(n); got != want {
			t.Errorf("ordinal(%d) = %s, want %s", n, got, want)
		}
	}
}

// The minor aspects are off unless asked for, and asking for them adds aspects
// without disturbing the ones already there.
func TestMinorAspectsAreOptional(t *testing.T) {
	birth := Birth{
		Name: "Reference",
		Year: 1961, Month: time.August, Day: 4, Hour: 19, Minute: 24,
		Zone: mustZone(t, "Pacific/Honolulu"), ZoneName: "Pacific/Honolulu",
		Latitude: 21.306944, Longitude: -157.858333,
	}

	plain, err := ComputeWith(birth, Options{})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}
	full, err := ComputeWith(birth, Options{MinorAspects: true})
	if err != nil {
		t.Fatalf("Compute: %v", err)
	}

	for _, a := range plain.Aspects {
		if a.Minor() {
			t.Errorf("%s appeared without being asked for", a.Name)
		}
	}
	if len(full.Aspects) <= len(plain.Aspects) {
		t.Errorf("asking for minor aspects gave %d aspects, no more than the %d without",
			len(full.Aspects), len(plain.Aspects))
	}

	// Every major aspect found without them is still found with them, unchanged:
	// the majors are searched first, so a pair near both keeps the major reading.
	majors := map[string]float64{}
	for _, a := range full.Aspects {
		if !a.Minor() {
			majors[a.A.Name()+a.Name+a.B.Name()] = a.Orb
		}
	}
	for _, a := range plain.Aspects {
		orb, ok := majors[a.A.Name()+a.Name+a.B.Name()]
		if !ok {
			t.Errorf("%s %s %s was lost when minor aspects were added", a.A.Name(), a.Name, a.B.Name())
			continue
		}
		if orb != a.Orb {
			t.Errorf("%s %s %s: orb changed from %v to %v", a.A.Name(), a.Name, a.B.Name(), a.Orb, orb)
		}
		var found bool
		for _, m := range minorAspects {
			if strings.EqualFold(m.name, a.Name) {
				found = true
			}
		}
		if found {
			t.Errorf("%s is listed as both a major and a minor aspect", a.Name)
		}
	}
}

// The rules a chart is exported under have to be the rules it was built under.
func TestAspectSetMatchesWhatIsSearched(t *testing.T) {
	for _, opt := range []Options{{}, {MinorAspects: true}} {
		set := AspectSet(opt)
		if got, want := len(set), len(aspectKindsFor(opt)); got != want {
			t.Fatalf("AspectSet(%+v) has %d entries, want %d", opt, got, want)
		}
		for i, k := range set {
			raw := aspectKindsFor(opt)[i]
			if k.Name != raw.name || k.Angle != raw.angle || k.Orb != raw.orb {
				t.Errorf("%s does not match the rule it was built from", k.Name)
			}
			if k.LuminaryOrb != raw.orb+raw.bonus {
				t.Errorf("%s: luminary orb %v, want %v", k.Name, k.LuminaryOrb, raw.orb+raw.bonus)
			}
			if k.LuminaryOrb <= k.Orb {
				t.Errorf("%s: the luminaries get no extra room", k.Name)
			}
			if k.Glyph == "" {
				t.Errorf("%s has no glyph", k.Name)
			}
			if k.Angle < 0 || k.Angle > 180 {
				t.Errorf("%s: angle %v is out of range", k.Name, k.Angle)
			}
		}
	}

	// The default set is the five Ptolemaic aspects, which is what the written
	// readings cover.
	if got, want := len(AspectSet(Options{})), 5; got != want {
		t.Errorf("the default set has %d aspects, want %d", got, want)
	}
	if got, want := len(AspectNames()), 5; got != want {
		t.Errorf("AspectNames has %d entries, want %d", got, want)
	}

	// No two aspects may claim overlapping bands, or which one a pair is reported
	// as would depend on the search order rather than on the geometry.
	set := AspectSet(Options{MinorAspects: true})
	for i := range set {
		for j := i + 1; j < len(set); j++ {
			gap := math.Abs(set[i].Angle - set[j].Angle)
			if reach := set[i].LuminaryOrb + set[j].LuminaryOrb; gap < reach {
				t.Errorf("%s and %s are %.0f° apart but reach %.0f° together",
					set[i].Name, set[j].Name, gap, reach)
			}
		}
	}
}

// Every aspect the chart can report has to have a nature, and the natures have
// to be the ones consumers are told to expect.
func TestEveryAspectHasANature(t *testing.T) {
	known := map[string]bool{
		"conjunction": true, "harmonious": true, "challenging": true, "disjunct": true,
	}
	for _, k := range AspectSet(Options{MinorAspects: true}) {
		nature := Aspect{Name: k.Name}.Nature()
		if !known[nature] {
			t.Errorf("%s has nature %q, which is not one of the four", k.Name, nature)
		}
		if k.Name != "Conjunction" && nature == "conjunction" {
			t.Errorf("%s fell through to the conjunction default", k.Name)
		}
	}
}
