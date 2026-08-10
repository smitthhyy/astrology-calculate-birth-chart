package web

import (
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"strings"
	"testing"
	_ "time/tzdata"

	"astronomyCalculator/internal/astro"
)

// exportFor casts the reference chart and returns the download, decoded both as
// the Go type and as free-form JSON. The second is what actually matters: the
// point of this document is that another program can read it, and a consumer
// will be looking for key names, not Go fields.
func exportFor(t *testing.T, query string) (chartExport, map[string]any) {
	t.Helper()
	rec := get(t, newTestServer(t, false), "/chart.json?"+query)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var typed chartExport
	if err := json.Unmarshal(rec.Body.Bytes(), &typed); err != nil {
		t.Fatalf("decoding into chartExport: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decoding as json: %v", err)
	}
	return typed, raw
}

func referenceExport(t *testing.T) (chartExport, map[string]any) {
	t.Helper()
	return exportFor(t, referenceForm().Encode())
}

func TestChartDownloadIsAnAttachment(t *testing.T) {
	rec := get(t, newTestServer(t, false), "/chart.json?"+referenceForm().Encode())
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("content type %q, want application/json", ct)
	}
	if cd := rec.Header().Get("Content-Disposition"); cd != `attachment; filename="test-birth-chart.json"` {
		t.Errorf("content disposition %q", cd)
	}
}

// The link on the result page has to point at a route that exists and carry
// enough of the form to recast the chart.
func TestResultPageLinksToTheJSONDownload(t *testing.T) {
	rec := post(t, newTestServer(t, false), "/chart", referenceForm())
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Download the data as JSON") {
		t.Fatal("the result page does not offer the JSON download")
	}
	if !strings.Contains(body, "/chart.json?") {
		t.Error("the JSON download link does not carry the birth details")
	}
}

func TestChartDownloadRejectsIncompleteDetails(t *testing.T) {
	rec := get(t, newTestServer(t, false), "/chart.json?name=Test")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("status %d, want 422", rec.Code)
	}
}

// Every field a consumer would join or key on, checked by its JSON name rather
// than through the Go struct — renaming a tag is exactly the kind of change
// that breaks a downstream reader silently.
func TestExportHasTheDocumentedShape(t *testing.T) {
	_, raw := referenceExport(t)

	for _, key := range []string{
		"$schema", "format", "formatVersion", "version", "generatedAt", "generator",
		"calculationSettings", "rawInput", "birth", "birthDataQuality",
		"calculatedChart", "interpretations", "warnings",
	} {
		if _, ok := raw[key]; !ok {
			t.Errorf("the document has no %q", key)
		}
	}
	if raw["format"] != exportFormat {
		t.Errorf("format %v, want %q", raw["format"], exportFormat)
	}

	object := func(parent map[string]any, key string) map[string]any {
		t.Helper()
		child, ok := parent[key].(map[string]any)
		if !ok {
			t.Fatalf("%q is %T, want an object", key, parent[key])
		}
		return child
	}
	require := func(parent map[string]any, name string, keys ...string) {
		t.Helper()
		for _, key := range keys {
			if _, ok := parent[key]; !ok {
				t.Errorf("%s has no %q", name, key)
			}
		}
	}

	require(object(raw, "generator"), "generator",
		"application", "applicationVersion", "calculationEngineVersion",
		"ephemerides", "ephemerisVersions", "timeZoneDatabase", "formatting")
	require(object(raw, "calculationSettings"), "calculationSettings",
		"zodiac", "houseSystem", "nodeType", "lilithType", "rulershipSystem",
		"observerPerspective", "coordinateReference", "aspectRules",
		"aspectCalculation", "housePlacementRules")
	require(object(raw, "birth"), "birth",
		"local", "utc", "timeZone", "place", "julianDay", "siderealTime")
	require(object(raw, "birthDataQuality"), "birthDataQuality",
		"timeStatus", "source", "timeAccuracySeconds", "rectified",
		"locationSource", "locationAccuracyMetres")

	chart := object(raw, "calculatedChart")
	require(chart, "calculatedChart",
		"houseSystem", "sect", "angles", "points", "derivedPoints", "houses",
		"aspects", "moonPhase", "chartRuler", "distribution")

	point := chart["points"].([]any)[0].(map[string]any)
	require(point, "a point",
		"key", "name", "glyph", "category", "longitudeDegrees", "position",
		"coordinates", "decan", "sign", "house", "speedPerDay", "retrograde")
	require(object(point, "position"), "a position",
		"sign", "signKey", "degree", "minute", "second", "degreeInSignDegrees", "formatted")
	require(object(point, "coordinates"), "a point's coordinates",
		"eclipticLongitudeDegrees", "eclipticLatitudeDegrees",
		"rightAscensionDegrees", "declinationDegrees")
	require(object(point, "sign"), "a sign",
		"key", "name", "glyph", "number", "element", "modality", "polarity", "ruler", "rulers")

	house := chart["houses"].([]any)[0].(map[string]any)
	require(house, "a house", "number", "cusp", "sign", "ruler", "occupants")
	require(object(house, "cusp"), "a house cusp", "longitudeDegrees", "position")

	aspect := chart["aspects"].([]any)[0].(map[string]any)
	require(aspect, "an aspect",
		"id", "between", "a", "b", "aspect", "aspectKey", "glyph", "nature", "minor",
		"exactAngleDegrees", "separationDegrees", "orbDegrees", "orbFormatted",
		"orbAllowedDegrees", "applying")

	require(object(raw, "interpretations"), "interpretations",
		"locale", "system", "interpretationVersion", "generatedBy", "points", "houses", "aspects")
}

// The calculated chart holds no written text, and the interpretations hold no
// numbers. This is the whole point of the split: a program analysing the chart
// should not have to step over prose, and an author replacing the readings
// should not be able to disturb a position.
func TestCalculationsAndInterpretationsAreSeparate(t *testing.T) {
	_, raw := referenceExport(t)

	chart := raw["calculatedChart"].(map[string]any)
	editorial := map[string]bool{
		"interpretation": true, "showsUp": true, "governs": true,
		"defines": true, "definition": true, "meaning": true,
	}
	var walk func(node any, path string)
	walk = func(node any, path string) {
		switch value := node.(type) {
		case map[string]any:
			for key, child := range value {
				if editorial[key] {
					t.Errorf("%s.%s is written interpretation inside the calculated chart", path, key)
				}
				walk(child, path+"."+key)
			}
		case []any:
			for _, child := range value {
				walk(child, path+"[]")
			}
		}
	}
	walk(chart, "calculatedChart")

	// And the readings must be keyed by the same keys the chart uses, or the two
	// halves cannot be put back together.
	readings := raw["interpretations"].(map[string]any)
	pointKeys := map[string]bool{}
	for _, entry := range chart["points"].([]any) {
		pointKeys[entry.(map[string]any)["key"].(string)] = true
	}
	for key := range readings["points"].(map[string]any) {
		if !pointKeys[key] {
			t.Errorf("there is a reading for %q, which is not a point in the chart", key)
		}
	}
	aspectIDs := map[string]bool{}
	for _, entry := range chart["aspects"].([]any) {
		aspectIDs[entry.(map[string]any)["id"].(string)] = true
	}
	for id := range readings["aspects"].(map[string]any) {
		if !aspectIDs[id] {
			t.Errorf("there is a reading for aspect %q, which is not in the chart", id)
		}
	}
	if len(readings["houses"].(map[string]any)) != 12 {
		t.Errorf("got readings for %d houses, want 12", len(readings["houses"].(map[string]any)))
	}
}

// An empty result set must be an empty array, not null. A consumer iterating
// over warnings should not have to distinguish "none" from "the field was
// forgotten".
func TestExportUsesEmptyArraysRatherThanNull(t *testing.T) {
	_, raw := referenceExport(t)

	if _, ok := raw["warnings"].([]any); !ok {
		t.Errorf("warnings is %T, want an array", raw["warnings"])
	}
	chart := raw["calculatedChart"].(map[string]any)
	for _, house := range chart["houses"].([]any) {
		if _, ok := house.(map[string]any)["occupants"].([]any); !ok {
			t.Fatalf("an empty house has no occupants array: %v", house)
		}
	}
}

// The three tables have to join on the same key, or a consumer is reduced to
// matching display names.
func TestExportTablesCrossReferenceByKey(t *testing.T) {
	typed, _ := referenceExport(t)

	keys := map[string]pointJSON{}
	for _, p := range typed.Chart.Points {
		keys[p.Key] = p
	}
	if want := len(astro.TableBodies) + len(astro.PointBodies); len(keys) != want {
		t.Fatalf("got %d distinct point keys, want %d", len(keys), want)
	}

	for _, h := range typed.Chart.Houses {
		for _, occupant := range h.Occupants {
			p, ok := keys[occupant]
			if !ok {
				t.Errorf("house %d is occupied by %q, which is not a point", h.Number, occupant)
				continue
			}
			if p.House != h.Number {
				t.Errorf("%s is listed in house %d but placed in house %d", p.Key, h.Number, p.House)
			}
		}
	}

	for _, a := range typed.Chart.Aspects {
		if _, ok := keys[a.A]; !ok {
			t.Errorf("aspect names %q, which is not a point", a.A)
		}
		if _, ok := keys[a.B]; !ok {
			t.Errorf("aspect names %q, which is not a point", a.B)
		}
	}

	if r := typed.Chart.ChartRuler; r == nil {
		t.Error("no chart ruler")
	} else if _, ok := keys[r.RulerKey]; !ok {
		t.Errorf("the chart ruler %q is not a point", r.RulerKey)
	}
}

// The numeric and the human-readable form of a position have to be the same
// position. They are produced by different code paths, and a consumer that
// prints one while calculating with the other would never notice a drift.
func TestExportNumbersAndTextAgree(t *testing.T) {
	typed, _ := referenceExport(t)

	all := append(append([]pointJSON{}, typed.Chart.Points...), typed.Chart.DerivedPoints...)
	for _, p := range all {
		checkPosition(t, p.Key, p.LongitudeDegrees, p.Position)
		if p.Coordinates.EclipticLongitudeDegrees != p.LongitudeDegrees {
			t.Errorf("%s: coordinates say %.9f, longitudeDegrees says %.9f",
				p.Key, p.Coordinates.EclipticLongitudeDegrees, p.LongitudeDegrees)
		}
		if want := astro.SignOf(p.LongitudeDegrees).String(); p.Sign.Name != want {
			t.Errorf("%s: sign %q, want %q", p.Key, p.Sign.Name, want)
		}
	}

	for _, h := range typed.Chart.Houses {
		checkPosition(t, "house "+itoa(h.Number), h.Cusp.LongitudeDegrees, h.Cusp.Position)
	}

	// The angles block and the points table are two views of the same four
	// numbers.
	byKey := map[string]float64{}
	for _, p := range typed.Chart.Points {
		byKey[p.Key] = p.LongitudeDegrees
	}
	for key, got := range map[string]float64{
		"ascendant":  typed.Chart.Angles.Ascendant,
		"descendant": typed.Chart.Angles.Descendant,
		"midheaven":  typed.Chart.Angles.Midheaven,
		"imumCoeli":  typed.Chart.Angles.ImumCoeli,
	} {
		if math.Abs(byKey[key]-got) > 1e-9 {
			t.Errorf("%s: angles says %.9f, points says %.9f", key, got, byKey[key])
		}
	}
}

// checkPosition holds the broken-out parts of a position against the longitude
// they came from. The parts are what a consumer will display; the longitude is
// what it will calculate with, and the two drifting apart is the failure this
// document exists to make impossible.
func checkPosition(t *testing.T, name string, lon float64, pos positionJSON) {
	t.Helper()

	inSign := astro.DegreeInSign(lon)
	if math.Abs(pos.DegreeInSignDegrees-inSign) > 1e-9 {
		t.Errorf("%s: degreeInSignDegrees %.9f, want %.9f", name, pos.DegreeInSignDegrees, inSign)
	}
	// The three parts must add back up to the degree in sign exactly, to well
	// inside the arcsecond they are quoted to.
	if sum := float64(pos.Degree) + float64(pos.Minute)/60 + pos.Second/3600; math.Abs(sum-inSign) > 1e-9 {
		t.Errorf("%s: %d° %d' %.4f\" sums to %.9f, want %.9f",
			name, pos.Degree, pos.Minute, pos.Second, sum, inSign)
	}
	if want := astro.FormatPositionSeconds(lon); pos.Formatted != want {
		t.Errorf("%s: formatted %q, want %q for %.6f°", name, pos.Formatted, want, lon)
	}
	if want := astro.SignOf(lon); pos.Sign != want.String() || pos.SignKey != want.Key() {
		t.Errorf("%s: sign %q/%q, want %q/%q", name, pos.Sign, pos.SignKey, want.String(), want.Key())
	}
}

// Positions carry arcseconds, not just arcminutes. A chart quoted to the
// arcminute cannot be checked against an ephemeris, and rounding at the
// arcminute would sometimes print 30°00' — a degree belonging to the next sign.
func TestExportPositionsCarryArcseconds(t *testing.T) {
	typed, raw := referenceExport(t)

	formatting := raw["generator"].(map[string]any)["formatting"].(map[string]any)
	if formatting["displayPrecision"] != "arcsecond" {
		t.Errorf("displayPrecision %v, want arcsecond", formatting["displayPrecision"])
	}
	if formatting["displayRounding"] != "truncate" {
		t.Errorf("displayRounding %v, want truncate", formatting["displayRounding"])
	}

	var withSeconds int
	for _, p := range typed.Chart.Points {
		if !strings.Contains(p.Position.Formatted, `"`) {
			t.Errorf("%s: %q carries no arcseconds", p.Key, p.Position.Formatted)
		}
		if p.Position.Second != 0 {
			withSeconds++
		}
	}
	// If every arcsecond came out zero the field would be decorative.
	if withSeconds == 0 {
		t.Error("no point has a non-zero arcsecond, so the extra precision is not real")
	}
}

// Declination is not recoverable from ecliptic longitude alone, and it is what
// parallels are read from. Both equatorial coordinates have to be right, not
// merely present.
func TestExportCarriesEquatorialCoordinates(t *testing.T) {
	typed, _ := referenceExport(t)

	obliquity := typed.Birth.Sidereal.ObliquityDegrees
	if obliquity < 23 || obliquity > 24 {
		t.Fatalf("obliquity %.6f° is not plausible", obliquity)
	}

	for _, p := range typed.Chart.Points {
		c := p.Coordinates
		if c.RightAscensionDegrees < 0 || c.RightAscensionDegrees >= 360 {
			t.Errorf("%s: right ascension %.6f° out of range", p.Key, c.RightAscensionDegrees)
		}
		if math.Abs(c.DeclinationDegrees) > 90 {
			t.Errorf("%s: declination %.6f° out of range", p.Key, c.DeclinationDegrees)
		}
		// Nothing on the ecliptic can be further from the equator than the
		// obliquity, plus whatever ecliptic latitude it has of its own.
		if limit := obliquity + math.Abs(c.EclipticLatitudeDegrees) + 1e-6; math.Abs(c.DeclinationDegrees) > limit {
			t.Errorf("%s: declination %.6f° exceeds the %.6f° the ecliptic allows",
				p.Key, c.DeclinationDegrees, limit)
		}
	}

	// The Midheaven's right ascension is the local sidereal time by definition,
	// which ties the equatorial coordinates to the chart's own time.
	for _, p := range typed.Chart.Points {
		if p.Key != "midheaven" {
			continue
		}
		if diff := math.Abs(p.Coordinates.RightAscensionDegrees - typed.Birth.Sidereal.LocalApparentDegrees); diff > 1e-6 && math.Abs(diff-360) > 1e-6 {
			t.Errorf("the Midheaven's right ascension is %.6f° but the sidereal time is %.6f°",
				p.Coordinates.RightAscensionDegrees, typed.Birth.Sidereal.LocalApparentDegrees)
		}
	}
}

// Nothing unknown may be reported as a zero, and nothing unknown may be left
// out either. The angles have no rate of motion in this chart, so they carry a
// null speed and no aspect to them is marked applying or separating — null being
// distinguishable from false, which an absent key is not.
func TestExportSaysNullForWhatItDoesNotKnow(t *testing.T) {
	typed, raw := referenceExport(t)

	chart := raw["calculatedChart"].(map[string]any)
	angleKeys := map[string]bool{"ascendant": true, "descendant": true, "midheaven": true, "imumCoeli": true}

	for _, entry := range chart["points"].([]any) {
		p := entry.(map[string]any)
		key := p["key"].(string)
		speed, present := p["speedPerDay"]
		if !present {
			t.Errorf("%s has no speedPerDay key at all", key)
			continue
		}
		if angleKeys[key] && speed != nil {
			t.Errorf("%s reports a speed of %v, but the chart holds no rate for the angles", key, speed)
		}
		if !angleKeys[key] && speed == nil {
			t.Errorf("%s reports no speed", key)
		}
	}
	// The derived points are arithmetic on the rest of the chart, so they have no
	// rate of their own either.
	for _, entry := range chart["derivedPoints"].([]any) {
		p := entry.(map[string]any)
		if speed, present := p["speedPerDay"]; !present || speed != nil {
			t.Errorf("the derived point %v reports a speed of %v", p["key"], speed)
		}
	}

	var checkedAngle, checkedBody bool
	for i, entry := range chart["aspects"].([]any) {
		a := entry.(map[string]any)
		applying, present := a["applying"]
		if !present {
			t.Errorf("aspect %d has no applying key at all", i)
			continue
		}
		involvesAngle := angleKeys[a["a"].(string)] || angleKeys[a["b"].(string)]
		if involvesAngle && applying != nil {
			t.Errorf("aspect %d involves an angle but says applying is %v", i, applying)
		}
		if !involvesAngle && applying == nil {
			t.Errorf("aspect %d between two bodies says nothing about applying", i)
		}
		checkedAngle = checkedAngle || involvesAngle
		checkedBody = checkedBody || !involvesAngle
	}
	if !checkedAngle || !checkedBody {
		t.Error("the reference chart does not exercise both the angle and the body case")
	}

	if len(typed.Chart.Aspects) == 0 {
		t.Error("the reference chart produced no aspects")
	}
}

// Every placement carries its written reading, so a consumer never has to come
// back for the text.
func TestExportCarriesTheReadings(t *testing.T) {
	typed, _ := referenceExport(t)

	for _, p := range typed.Chart.Points {
		reading, ok := typed.Interpretations.Points[p.Key]
		if !ok {
			t.Errorf("%s has no reading", p.Key)
			continue
		}
		if reading.Defines == "" || reading.ShowsUp == "" {
			t.Errorf("%s has an incomplete reading: %+v", p.Key, reading)
		}
	}
	for _, h := range typed.Chart.Houses {
		reading, ok := typed.Interpretations.Houses[itoa(h.Number)]
		if !ok || reading.Governs == "" || reading.ShowsUp == "" {
			t.Errorf("house %d has no reading: %+v", h.Number, reading)
		}
	}
	for _, a := range typed.Chart.Aspects {
		if typed.Interpretations.Aspects[a.ID].ShowsUp == "" {
			t.Errorf("%s has no reading", a.ID)
		}
	}
	if typed.Interpretations.Locale == "" || typed.Interpretations.InterpretationVersion == "" {
		t.Error("the readings do not say what locale or version they are")
	}
}

func TestExportDistributionCountsEveryPlanetOnce(t *testing.T) {
	typed, _ := referenceExport(t)
	d := typed.Chart.Distribution

	const planets = 10
	if len(d.CountedPoints) != planets {
		t.Errorf("countedPoints lists %d points, want %d", len(d.CountedPoints), planets)
	}
	if d.Basis == "" {
		t.Error("the distribution does not say what it counted")
	}

	for name, counts := range map[string]map[string]int{
		"elements": d.Elements, "modalities": d.Modalities, "polarities": d.Polarities,
		"quadrants": d.Quadrants,
	} {
		total := 0
		for _, n := range counts {
			total += n
		}
		if total != planets {
			t.Errorf("%s total %d, want %d: %v", name, total, planets, counts)
		}
	}

	// Each planet is counted twice over the hemispheres, once on each axis.
	total := 0
	for _, n := range d.Hemispheres {
		total += n
	}
	if total != 2*planets {
		t.Errorf("hemisphere total %d, want %d: %v", total, 2*planets, d.Hemispheres)
	}
	if d.Hemispheres["northern"]+d.Hemispheres["southern"] != planets {
		t.Errorf("the horizon axis does not account for every planet: %v", d.Hemispheres)
	}
	if d.Hemispheres["eastern"]+d.Hemispheres["western"] != planets {
		t.Errorf("the meridian axis does not account for every planet: %v", d.Hemispheres)
	}
}

// The document says what it was cast under, so a consumer comparing it with a
// chart from elsewhere can see why they might differ.
func TestExportStatesItsOwnConventions(t *testing.T) {
	typed, _ := referenceExport(t)
	s := typed.Settings

	if s.HouseSystemName != "Placidus" || typed.Chart.HouseSystem != "Placidus" {
		t.Errorf("house system %q / %q", s.HouseSystemName, typed.Chart.HouseSystem)
	}
	if s.HouseSystem != "placidus" {
		t.Errorf("house system key %q, want placidus", s.HouseSystem)
	}
	for name, got := range map[string]string{
		"zodiac":              s.Zodiac,
		"nodeType":            s.NodeType,
		"rulershipSystem":     s.RulershipSystem,
		"observerPerspective": s.ObserverPerspective,
		"lilithType":          s.LilithType,
	} {
		if got == "" {
			t.Errorf("%s is not stated", name)
		}
	}
	// The lunar nodes this application computes are the true ones, which differ
	// from the mean by up to about a degree and a half — more than most orbs.
	if s.NodeType != "true" {
		t.Errorf("nodeType %q, want true", s.NodeType)
	}
	if s.CoordinateReference.Equinox == "" || s.CoordinateReference.Ecliptic == "" {
		t.Errorf("the coordinate reference is incomplete: %+v", s.CoordinateReference)
	}
	if s.HousePlacementRules.CuspConvention == "" {
		t.Error("the house placement rules do not say how a point is assigned to a house")
	}
}

// The version fields have to identify the format, the program and the
// calculation separately, because they move at different times and a consumer
// comparing two charts needs to know which of the three changed.
func TestExportStatesItsVersions(t *testing.T) {
	typed, raw := referenceExport(t)

	if raw["$schema"] != schemaPath {
		t.Errorf("$schema %v, want %q", raw["$schema"], schemaPath)
	}
	if typed.FormatVersion != exportFormatVersion {
		t.Errorf("formatVersion %q, want %q", typed.FormatVersion, exportFormatVersion)
	}
	// The integer version must be the major component of the semantic one, or the
	// two ways of asking the same question would give different answers.
	if want := strings.SplitN(exportFormatVersion, ".", 2)[0]; itoa(typed.Version) != want {
		t.Errorf("version %d does not match formatVersion %q", typed.Version, typed.FormatVersion)
	}
	if typed.Generator.CalculationEngineVersion != astro.EngineVersion {
		t.Errorf("calculationEngineVersion %q, want %q",
			typed.Generator.CalculationEngineVersion, astro.EngineVersion)
	}
	if typed.Generator.ApplicationVersion == "" {
		t.Error("the application version is not stated")
	}

	if len(typed.Generator.Ephemerides) == 0 {
		t.Fatal("no ephemeris is named")
	}
	for _, e := range typed.Generator.Ephemerides {
		if e.Key == "" || e.Name == "" || e.Version == "" || e.Source == "" {
			t.Errorf("an ephemeris is incompletely described: %+v", e)
		}
		if typed.Generator.EphemerisVersions[e.Name] != e.Version {
			t.Errorf("ephemerisVersions[%q] = %q, want %q",
				e.Name, typed.Generator.EphemerisVersions[e.Name], e.Version)
		}
	}

	// The zone database version cannot be read at run time. It must be an
	// explicit null with an explanation, never a guess.
	db := raw["generator"].(map[string]any)["timeZoneDatabase"].(map[string]any)
	if _, present := db["version"]; !present {
		t.Error("timeZoneDatabase has no version key")
	} else if db["version"] != nil {
		t.Errorf("timeZoneDatabase.version is %v, but no version is knowable at run time", db["version"])
	}
	if db["source"] == "" || db["name"] == "" {
		t.Errorf("the zone database is not identified: %v", db)
	}
}

// The aspect rules and what the chart actually contains have to agree, and the
// document has to say which points were compared. A consumer finding an aspect
// this chart omits otherwise cannot tell a disagreement about geometry from a
// disagreement about what counts as a point.
func TestExportStatesItsAspectRules(t *testing.T) {
	typed, _ := referenceExport(t)
	s := typed.Settings

	if len(s.AspectRules) != len(astro.AspectSet(astro.Options{})) {
		t.Errorf("got %d aspect rules, want %d", len(s.AspectRules), len(astro.AspectSet(astro.Options{})))
	}
	allowed := map[string]aspectRuleJSON{}
	for _, rule := range s.AspectRules {
		if rule.Key == "" || rule.Glyph == "" {
			t.Errorf("aspect rule %q is incomplete: %+v", rule.Name, rule)
		}
		if rule.LuminaryOrb < rule.Orb {
			t.Errorf("%s: luminary orb %v is tighter than the ordinary orb %v", rule.Name, rule.LuminaryOrb, rule.Orb)
		}
		allowed[rule.Name] = rule
	}

	points := map[string]bool{}
	for _, key := range s.AspectCalculation.IncludedPoints {
		points[key] = true
	}
	if len(points) == 0 {
		t.Fatal("the document does not say which points were compared")
	}
	if s.AspectCalculation.IncludeHouseCusps {
		t.Error("includeHouseCusps is true, but the cusps are not compared")
	}

	for _, a := range typed.Chart.Aspects {
		rule, ok := allowed[a.Aspect]
		if !ok {
			t.Errorf("%s is a %s, which is not in the stated rules", a.ID, a.Aspect)
			continue
		}
		limit := rule.Orb
		if a.A == "sun" || a.A == "moon" || a.B == "sun" || a.B == "moon" {
			limit = rule.LuminaryOrb
		}
		if a.OrbDegrees > limit {
			t.Errorf("%s has an orb of %.4f°, beyond the stated %.1f°", a.ID, a.OrbDegrees, limit)
		}
		if math.Abs(a.OrbAllowedDegrees-limit) > 1e-9 {
			t.Errorf("%s says %.4f° was allowed, but the rules say %.4f°", a.ID, a.OrbAllowedDegrees, limit)
		}
		if !points[a.A] || !points[a.B] {
			t.Errorf("%s is between points the document does not list as compared", a.ID)
		}
		if a.Minor {
			t.Errorf("%s is minor, but minor aspects were not asked for", a.ID)
		}
	}
}

// The minor aspects are off unless asked for. They are standard enough to be
// worth offering to a program, and there is no written reading for them, so
// turning them on by default would change every chart and improve none.
func TestMinorAspectsAreOptional(t *testing.T) {
	plain, _ := referenceExport(t)

	form := referenceForm()
	form.Set("minorAspects", "1")
	extended, _ := exportFor(t, form.Encode())

	if !extended.Settings.AspectCalculation.IncludeMinorAspects {
		t.Error("the extended chart does not say it includes minor aspects")
	}
	if plain.Settings.AspectCalculation.IncludeMinorAspects {
		t.Error("the plain chart says it includes minor aspects")
	}
	if len(extended.Settings.AspectRules) <= len(plain.Settings.AspectRules) {
		t.Errorf("asking for minor aspects did not add any rules: %d vs %d",
			len(extended.Settings.AspectRules), len(plain.Settings.AspectRules))
	}
	if len(extended.Chart.Aspects) <= len(plain.Chart.Aspects) {
		t.Errorf("asking for minor aspects found nothing new: %d vs %d",
			len(extended.Chart.Aspects), len(plain.Chart.Aspects))
	}

	// The majors must be exactly the same aspects, at exactly the same orbs. A
	// wider search must not disturb what was already there.
	was := map[string]float64{}
	for _, a := range plain.Chart.Aspects {
		was[a.ID] = a.OrbDegrees
	}
	var minors int
	for _, a := range extended.Chart.Aspects {
		if a.Minor {
			minors++
			continue
		}
		orb, ok := was[a.ID]
		if !ok {
			t.Errorf("%s appeared only once minor aspects were switched on, but is not minor", a.ID)
			continue
		}
		if orb != a.OrbDegrees {
			t.Errorf("%s: orb %.9f with minors, %.9f without", a.ID, a.OrbDegrees, orb)
		}
		delete(was, a.ID)
	}
	for id := range was {
		t.Errorf("%s disappeared when minor aspects were switched on", id)
	}
	if minors == 0 {
		t.Error("no aspect is marked minor")
	}
}

// The derived points travel with the chart, each with the formula it came from,
// and none of them is a house occupant or an aspect partner.
func TestExportCarriesTheDerivedPoints(t *testing.T) {
	typed, _ := referenceExport(t)

	if len(typed.Chart.DerivedPoints) != len(astro.DerivedBodies) {
		t.Errorf("got %d derived points, want %d", len(typed.Chart.DerivedPoints), len(astro.DerivedBodies))
	}

	derived := map[string]bool{}
	for _, p := range typed.Chart.DerivedPoints {
		derived[p.Key] = true
		if p.Derivation == nil {
			t.Errorf("%s does not say how it was derived", p.Key)
			continue
		}
		if p.Derivation.Formula == "" || p.Derivation.FormulaType == "" {
			t.Errorf("%s has an incomplete derivation: %+v", p.Key, *p.Derivation)
		}
	}
	if !derived["partOfFortune"] {
		t.Error("the Part of Fortune is missing")
	}

	// A derived point is not a body: it occupies no house and aspects nothing,
	// and the tables the reader sees must not have changed shape.
	for _, p := range typed.Chart.Points {
		if derived[p.Key] {
			t.Errorf("%s appears in both points and derivedPoints", p.Key)
		}
	}
	for _, h := range typed.Chart.Houses {
		for _, occupant := range h.Occupants {
			if derived[occupant] {
				t.Errorf("house %d lists the derived point %s as an occupant", h.Number, occupant)
			}
		}
	}
	for _, a := range typed.Chart.Aspects {
		if derived[a.A] || derived[a.B] {
			t.Errorf("%s aspects a derived point", a.ID)
		}
	}
}

// How well the birth time is known travels with the chart. Nothing else records
// it, and a chart cast from a guess has an Ascendant that means nothing.
func TestExportRecordsTheBirthDataQuality(t *testing.T) {
	t.Run("nothing said", func(t *testing.T) {
		typed, raw := referenceExport(t)
		q := typed.BirthDataQuality

		if q.TimeStatus != string(astro.TimeUnstated) {
			t.Errorf("timeStatus %q, want %q", q.TimeStatus, astro.TimeUnstated)
		}
		if q.Rectified {
			t.Error("a chart with nothing said about its time is marked rectified")
		}
		// Saying nothing must not be reported as an accuracy, and must not be
		// silently upgraded to "recorded".
		quality := raw["birthDataQuality"].(map[string]any)
		if quality["timeAccuracySeconds"] != nil {
			t.Errorf("timeAccuracySeconds is %v, but nothing was said about the time",
				quality["timeAccuracySeconds"])
		}
		if quality["source"] != nil {
			t.Errorf("source is %v, but none was given", quality["source"])
		}
		// A gazetteer point carries no radius, so no accuracy may be invented.
		if quality["locationAccuracyMetres"] != nil {
			t.Errorf("locationAccuracyMetres is %v, but it cannot be derived",
				quality["locationAccuracyMetres"])
		}
		if q.LocationSource == "" {
			t.Error("the location source is not stated")
		}
	})

	t.Run("recorded", func(t *testing.T) {
		form := referenceForm()
		form.Set("timeStatus", "recorded")
		form.Set("timeSource", "birth certificate")

		typed, _ := exportFor(t, form.Encode())
		q := typed.BirthDataQuality
		if q.TimeStatus != "recorded" {
			t.Errorf("timeStatus %q, want recorded", q.TimeStatus)
		}
		if q.Source == nil || *q.Source != "birth certificate" {
			t.Errorf("source %v, want the birth certificate", q.Source)
		}
		// A recorded time is known to the granularity of the form and no better.
		if q.TimeAccuracySeconds == nil || *q.TimeAccuracySeconds != 60 {
			t.Errorf("timeAccuracySeconds %v, want 60", q.TimeAccuracySeconds)
		}
		if q.TimeStatusLabel == "" {
			t.Error("the status has no label")
		}
		// Nothing doubtful was said, so nothing should be warned about.
		for _, w := range typed.Warnings {
			if strings.HasPrefix(w.Code, "BIRTH_TIME_") {
				t.Errorf("a recorded time produced %s", w.Code)
			}
		}
	})

	t.Run("rectified is flagged separately", func(t *testing.T) {
		form := referenceForm()
		form.Set("timeStatus", "rectified")

		typed, _ := exportFor(t, form.Encode())
		if !typed.BirthDataQuality.Rectified {
			t.Error("a rectified chart is not marked as one")
		}
		if typed.BirthDataQuality.TimeAccuracySeconds != nil {
			t.Error("a rectified time claims an accuracy")
		}
	})

	t.Run("a doubtful time warns", func(t *testing.T) {
		for _, status := range []string{"approximate", "unknown"} {
			form := referenceForm()
			form.Set("timeStatus", status)

			typed, _ := exportFor(t, form.Encode())
			var found bool
			for _, w := range typed.Warnings {
				if strings.HasPrefix(w.Code, "BIRTH_TIME_") {
					found = true
					if len(w.AffectedFields) == 0 {
						t.Errorf("%s: %s names no affected fields", status, w.Code)
					}
				}
			}
			if !found {
				t.Errorf("%s: a doubtful birth time produced no warning", status)
			}
		}
	})
}

// The input travels with the chart verbatim, so the document can be fed back in
// to reproduce itself.
func TestExportRoundTripsItsOwnInput(t *testing.T) {
	first, _ := referenceExport(t)

	form := referenceForm()
	form.Set("name", first.RawInput.Name)
	form.Set("date", first.RawInput.Date)
	form.Set("time", first.RawInput.Time)
	form.Set("zone", first.RawInput.Zone)
	form.Set("location", first.RawInput.Location)
	form.Set("country", first.RawInput.Country)
	form.Set("latitude", first.RawInput.Latitude)
	form.Set("longitude", first.RawInput.Longitude)

	second, _ := exportFor(t, form.Encode())
	if first.Birth.UTC != second.Birth.UTC {
		t.Errorf("recast from its own input gives %s, want %s", second.Birth.UTC, first.Birth.UTC)
	}
	for i, p := range first.Chart.Points {
		if p.LongitudeDegrees != second.Chart.Points[i].LongitudeDegrees {
			t.Errorf("%s moved on the round trip", p.Key)
		}
	}
}

// The warnings are part of the data, and structured so a program can act on them
// without reading English. A chart cast against a doubtful time zone has to say
// so wherever it is read, not only on the page.
func TestExportCarriesTheWarnings(t *testing.T) {
	form := referenceForm()
	form.Set("zone", "America/New_York")

	typed, _ := exportFor(t, form.Encode())
	if len(typed.Warnings) == 0 {
		t.Fatal("a mismatched time zone produced no warning in the export")
	}

	known := map[string]bool{}
	for _, field := range astro.WarningFields() {
		known[field] = true
	}

	var found bool
	for _, w := range typed.Warnings {
		if w.Code == "" || w.Message == "" {
			t.Errorf("a warning is incomplete: %+v", w)
		}
		if w.Severity != astro.SeverityWarning && w.Severity != astro.SeverityInfo {
			t.Errorf("%s has severity %q", w.Code, w.Severity)
		}
		for _, field := range w.AffectedFields {
			if !known[field] {
				t.Errorf("%s names the field %q, which is not one of the documented paths", w.Code, field)
			}
		}
		if w.Code == astro.WarnTimeZoneMismatch {
			found = true
			if w.Severity != astro.SeverityWarning {
				t.Errorf("a wrong time zone is only %q", w.Severity)
			}
			if !strings.Contains(w.Message, "nearest place") {
				t.Errorf("the zone warning does not explain itself: %q", w.Message)
			}
		}
	}
	if !found {
		t.Errorf("no %s warning: %+v", astro.WarnTimeZoneMismatch, typed.Warnings)
	}
}

// Every path a warning names must be a real place in the document, or a consumer
// told which fields to distrust cannot find them.
func TestWarningFieldsPointAtRealPlaces(t *testing.T) {
	_, raw := referenceExport(t)

	for _, path := range astro.WarningFields() {
		node := any(raw)
		for _, segment := range strings.Split(path, ".") {
			object, ok := node.(map[string]any)
			if !ok {
				t.Errorf("%s: %q is not an object", path, segment)
				break
			}
			child, present := object[segment]
			if !present {
				t.Errorf("%s names %q, which the document does not contain", path, segment)
				break
			}
			node = child
		}
	}
}

func TestExportFilename(t *testing.T) {
	tests := map[string]string{
		"Test":                  "test-birth-chart.json",
		"Mary Jane":             "mary-jane-birth-chart.json",
		"  O'Brien  ":           "o-brien-birth-chart.json",
		"Ada-Lovelace":          "ada-lovelace-birth-chart.json",
		"":                      "birth-chart.json",
		"...":                   "birth-chart.json",
		strings.Repeat("a", 60): strings.Repeat("a", 40) + "-birth-chart.json",
	}
	for in, want := range tests {
		if got := exportFilename(in); got != want {
			t.Errorf("exportFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestKeyOf(t *testing.T) {
	tests := map[string]string{
		"Whole Sign":      "whole-sign",
		"Square":          "square",
		"Placidus":        "placidus",
		"Semisextile":     "semisextile",
		"northNode":       "northnode",
		"Part of Fortune": "part-of-fortune",
		"  spaced  ":      "spaced",
		"":                "",
	}
	for in, want := range tests {
		if got := keyOf(in); got != want {
			t.Errorf("keyOf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSiderealHours(t *testing.T) {
	tests := map[float64]string{
		0: "00h00m00s", 15: "01h00m00s", 180: "12h00m00s", 114.5531111: "07h38m12s",
	}
	for deg, want := range tests {
		if got := siderealHours(deg); got != want {
			t.Errorf("siderealHours(%v) = %q, want %q", deg, got, want)
		}
	}
}

func TestOffsetLabel(t *testing.T) {
	tests := map[int]string{
		0: "+00:00", 36000: "+10:00", 34200: "+09:30", -18000: "-05:00", -5280: "-01:28",
	}
	for seconds, want := range tests {
		if got := offsetLabel(seconds); got != want {
			t.Errorf("offsetLabel(%d) = %q, want %q", seconds, got, want)
		}
	}
}

// itoa keeps the house-number-to-map-key conversions in the tests readable.
func itoa(n int) string { return strconv.Itoa(n) }
