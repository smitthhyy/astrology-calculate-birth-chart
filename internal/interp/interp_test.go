package interp

import (
	"strings"
	"testing"
	"unicode"
)

// requiredBodies are the chart points that must carry a full set of readings:
// every planet plus the Ascendant and Descendant.
var requiredBodies = []string{
	"sun", "moon", "mercury", "venus", "mars", "jupiter", "saturn",
	"uranus", "neptune", "pluto", "ascendant", "descendant",
}

func TestDataIsComplete(t *testing.T) {
	if err := Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	for _, body := range requiredBodies {
		for _, sign := range signKeys {
			b, r, ok := Lookup(body, sign)
			if !ok {
				t.Errorf("%s in %s: missing", body, sign)
				continue
			}
			if b.Name == "" {
				t.Errorf("%s: no display name", body)
			}
			if len(r.Definition) < 40 {
				t.Errorf("%s in %s: definition is only %d characters", body, sign, len(r.Definition))
			}
			if len(r.ShowsUp) < 40 {
				t.Errorf("%s in %s: showsUp is only %d characters", body, sign, len(r.ShowsUp))
			}
		}
	}
}

func TestNoBodyRepeatsAReading(t *testing.T) {
	for _, body := range requiredBodies {
		seen := map[string]string{}
		for _, sign := range signKeys {
			_, r, ok := Lookup(body, sign)
			if !ok {
				continue
			}
			for label, text := range map[string]string{"definition": r.Definition, "showsUp": r.ShowsUp} {
				key := label + "\x00" + text
				if prev, dup := seen[key]; dup {
					t.Errorf("%s: %s for %s is identical to %s", body, label, sign, prev)
				}
				seen[key] = sign
			}
		}
	}
}

func TestDefines(t *testing.T) {
	for _, body := range requiredBodies {
		d, ok := Defines(body)
		if !ok {
			t.Errorf("%s: no defines line", body)
			continue
		}
		if !strings.HasSuffix(strings.TrimSpace(d), ".") {
			t.Errorf("%s: defines line does not end in a full stop: %q", body, d)
		}
	}
	if _, ok := Defines("midheaven"); ok {
		t.Error("midheaven should have no written text")
	}
}

func TestBodiesCoversTheRequiredSet(t *testing.T) {
	have := map[string]bool{}
	for _, k := range Bodies() {
		have[k] = true
	}
	for _, body := range requiredBodies {
		if !have[body] {
			t.Errorf("%s is missing from the data set", body)
		}
	}
	if len(Bodies()) != len(requiredBodies) {
		t.Errorf("got %d bodies, want %d", len(Bodies()), len(requiredBodies))
	}
}

func TestHouseDataIsComplete(t *testing.T) {
	for house := 1; house <= 12; house++ {
		for _, sign := range signKeys {
			text, ok := HouseReading(house, sign)
			if !ok {
				t.Errorf("house %d in %s: missing", house, sign)
				continue
			}
			if len(text) < 40 {
				t.Errorf("house %d in %s: only %d characters", house, sign, len(text))
			}
		}
	}
}

func TestHouseReadingRejectsUnknownInput(t *testing.T) {
	if _, ok := HouseReading(0, "aries"); ok {
		t.Error("house 0 should not resolve")
	}
	if _, ok := HouseReading(13, "aries"); ok {
		t.Error("house 13 should not resolve")
	}
	if _, ok := HouseReading(1, "ophiuchus"); ok {
		t.Error("an unknown sign should not resolve")
	}
}

func TestNoHouseRepeatsAReading(t *testing.T) {
	for house := 1; house <= 12; house++ {
		seen := map[string]string{}
		for _, sign := range signKeys {
			text, ok := HouseReading(house, sign)
			if !ok {
				continue
			}
			if prev, dup := seen[text]; dup {
				t.Errorf("house %d: the %s reading is identical to %s", house, sign, prev)
			}
			seen[text] = sign
		}
	}
}

// A chart shows a house reading and a body reading side by side whenever a
// body happens to sit in the sign on that house's cusp — and for the first and
// seventh houses, whose cusps are the Ascendant and Descendant, that is always.
// Both are written from the same astrological association, so it is easy to
// reach for the same phrasing twice; a long verbatim run means one was
// paraphrased from the other and the pair will read as padding.
func TestHouseReadingsDoNotEchoTheBodies(t *testing.T) {
	// Five words of ordinary English ("and you are drawn to") is coincidence.
	// Six or more is not.
	const maxRun = 6

	for house := 1; house <= 12; house++ {
		for _, sign := range signKeys {
			houseText, ok := HouseReading(house, sign)
			if !ok {
				continue
			}
			for _, key := range requiredBodies {
				_, reading, ok := Lookup(key, sign)
				if !ok {
					continue
				}
				for label, bodyText := range map[string]string{
					"definition": reading.Definition,
					"showsUp":    reading.ShowsUp,
				} {
					if run := longestSharedRun(houseText, bodyText); len(run) >= maxRun {
						t.Errorf("house %d in %s shares %d words with %s.%s: %q",
							house, sign, len(run), key, label, strings.Join(run, " "))
					}
				}
			}
		}
	}
}

// requiredPoints are the secondary chart points shown in the "Other points"
// table. Unlike the planets they carry one line per sign rather than two.
var requiredPoints = []string{"midheaven", "imumCoeli", "northNode", "southNode"}

func TestPointDataIsComplete(t *testing.T) {
	for _, point := range requiredPoints {
		d, ok := PointDefines(point)
		if !ok {
			t.Errorf("%s: no defines line", point)
			continue
		}
		if !strings.HasSuffix(strings.TrimSpace(d), ".") {
			t.Errorf("%s: defines line does not end in a full stop: %q", point, d)
		}
		for _, sign := range signKeys {
			text, ok := PointReading(point, sign)
			if !ok {
				t.Errorf("%s in %s: missing", point, sign)
				continue
			}
			if len(text) < 40 {
				t.Errorf("%s in %s: only %d characters", point, sign, len(text))
			}
		}
	}
	if len(Points()) != len(requiredPoints) {
		t.Errorf("got %d points, want %d", len(Points()), len(requiredPoints))
	}
	// The planets and the secondary points are looked up through different
	// functions, and mixing the two would silently blank a column.
	if _, ok := PointReading("sun", "aries"); ok {
		t.Error("the Sun should not resolve as a secondary point")
	}
	if _, _, ok := Lookup("midheaven", "aries"); ok {
		t.Error("the Midheaven should not resolve as a body")
	}
}

func TestNoPointRepeatsAReading(t *testing.T) {
	for _, point := range requiredPoints {
		seen := map[string]string{}
		for _, sign := range signKeys {
			text, ok := PointReading(point, sign)
			if !ok {
				continue
			}
			if prev, dup := seen[text]; dup {
				t.Errorf("%s: the %s reading is identical to %s", point, sign, prev)
			}
			seen[text] = sign
		}
	}
}

// In Placidus the Midheaven is the tenth house cusp and the Imum Coeli the
// fourth, so those two pairs of readings are guaranteed to appear on the same
// page carrying the same sign. They cover the same ground — career, and home —
// which makes it easy to write the second as a paraphrase of the first and
// leave the reader with two cells saying one thing.
func TestPointReadingsDoNotEchoTheHouseOnTheSameCusp(t *testing.T) {
	const maxRun = 6

	for point, house := range map[string]int{"midheaven": 10, "imumCoeli": 4} {
		for _, sign := range signKeys {
			pointText, ok := PointReading(point, sign)
			if !ok {
				continue
			}
			houseText, ok := HouseReading(house, sign)
			if !ok {
				continue
			}
			if run := longestSharedRun(pointText, houseText); len(run) >= maxRun {
				t.Errorf("%s in %s shares %d words with house %d: %q",
					point, sign, len(run), house, strings.Join(run, " "))
			}
		}
	}
}

// aspectNames are the five Ptolemaic aspects as astro names them. They are
// spelled out here rather than imported so that this package stays independent
// of the ephemeris; internal/web holds the test that the two agree.
var aspectNames = []string{"Conjunction", "Opposition", "Trine", "Square", "Sextile"}

func TestAspectDataIsComplete(t *testing.T) {
	if err := Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	pairs, err := loadAspects()
	if err != nil {
		t.Fatalf("loadAspects: %v", err)
	}
	// Twelve bodies form aspects, so there are 12 × 11 / 2 pairs.
	if len(pairs) != 66 {
		t.Errorf("got %d body pairs, want 66", len(pairs))
	}
	for key, readings := range pairs {
		for _, mode := range aspectModes {
			if len(readings[mode]) < 40 {
				t.Errorf("%q: the %s reading is only %d characters",
					strings.ReplaceAll(key, "\x00", " and "), mode, len(readings[mode]))
			}
		}
	}
}

func TestNoAspectReadingIsRepeated(t *testing.T) {
	pairs, err := loadAspects()
	if err != nil {
		t.Fatalf("loadAspects: %v", err)
	}
	seen := map[string]string{}
	for key, readings := range pairs {
		for _, mode := range aspectModes {
			label := strings.ReplaceAll(key, "\x00", " and ") + ", " + mode
			if prev, dup := seen[readings[mode]]; dup {
				t.Errorf("%s is identical to %s", label, prev)
			}
			seen[readings[mode]] = label
		}
	}
}

// The lookup has to work whichever way round the pair is named, because the
// caller reports the aspect in whatever order the two bodies came out of the
// chart.
func TestAspectReadingIsOrderIndependent(t *testing.T) {
	for _, name := range aspectNames {
		forwards, ok := AspectReading("sun", "saturn", name)
		if !ok {
			t.Fatalf("no %s reading for the Sun and Saturn", name)
		}
		backwards, ok := AspectReading("saturn", "sun", name)
		if !ok {
			t.Fatalf("no %s reading for Saturn and the Sun", name)
		}
		if forwards != backwards {
			t.Errorf("%s: the two orderings give different readings", name)
		}
	}
}

// The trine and the sextile deliberately share a reading. This pins that as a
// decision rather than an oversight: see the comment on the mode constants.
func TestTrineAndSextileShareAReading(t *testing.T) {
	trine, ok := AspectReading("moon", "mars", "Trine")
	if !ok {
		t.Fatal("no trine reading for the Moon and Mars")
	}
	sextile, _ := AspectReading("moon", "mars", "Sextile")
	if trine != sextile {
		t.Error("the trine and the sextile should share the harmonious reading")
	}
	square, _ := AspectReading("moon", "mars", "Square")
	opposition, _ := AspectReading("moon", "mars", "Opposition")
	if square == opposition {
		t.Error("the square and the opposition should not share a reading")
	}
}

func TestAspectReadingRejectsUnknownInput(t *testing.T) {
	if _, ok := AspectReading("sun", "moon", "Quincunx"); ok {
		t.Error("an aspect with no written tone should not resolve")
	}
	if _, ok := AspectReading("sun", "chiron", "Trine"); ok {
		t.Error("an unknown body should not resolve")
	}
	// A body never forms an aspect with itself.
	if _, ok := AspectReading("sun", "sun", "Conjunction"); ok {
		t.Error("a body paired with itself should not resolve")
	}
}

func longestSharedRun(a, b string) []string {
	wordsA, wordsB := words(a), words(b)
	var best []string
	for i := range wordsA {
		for j := range wordsB {
			n := 0
			for i+n < len(wordsA) && j+n < len(wordsB) && wordsA[i+n] == wordsB[j+n] {
				n++
			}
			if n > len(best) {
				best = wordsA[i : i+n]
			}
		}
	}
	return best
}

func words(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
}
