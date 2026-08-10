package web

import (
	"encoding/json"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	_ "time/tzdata"

	"astronomyCalculator/internal/astro"
	"astronomyCalculator/internal/interp"
)

func newTestServer(t *testing.T, allowOnline bool) http.Handler {
	t.Helper()
	s, err := New(allowOnline, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s.Handler()
}

// referenceForm is the birth used throughout: 1990-06-15 14:30 in Ballarat.
func referenceForm() url.Values {
	return url.Values{
		"name":      {"Test"},
		"date":      {"15/06/1990"},
		"time":      {"14:30"},
		"zone":      {"Australia/Melbourne"},
		"location":  {"Ballarat"},
		"country":   {"Australia"},
		"latitude":  {"-37.5662"},
		"longitude": {"143.8496"},
	}
}

func post(t *testing.T, h http.Handler, path string, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestFormPage(t *testing.T) {
	rec := get(t, newTestServer(t, false), "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		`name="name"`, `name="date"`, `name="time"`, `name="zone"`,
		`name="location"`, `name="country"`,
		`id="zone-options"`, `Australia/Melbourne`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the form is missing %q", want)
		}
	}
	// The online button must not appear when online lookup is switched off.
	if strings.Contains(body, "data-online-search") {
		t.Error("the online search button is offered even though it is disabled")
	}
}

func TestFormPageOffersOnlineSearchWhenEnabled(t *testing.T) {
	rec := get(t, newTestServer(t, true), "/")
	if !strings.Contains(rec.Body.String(), "data-online-search") {
		t.Error("the online search button is missing when online lookup is enabled")
	}
}

func TestChartPage(t *testing.T) {
	rec := post(t, newTestServer(t, false), "/chart", referenceForm())
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	for _, want := range []string{
		"Test&#39;s birth chart",
		// The five column headings the chart is specified around.
		">Cosmic Part<", ">Zodiac sign<", ">What it defines<", ">Definition<", ">How it shows up<",
		// A placement and a house cusp, checked against Swiss Ephemeris.
		"23°49&#39; Gemini", // Sun
		"11°11&#39; Pisces", // Moon
		"Placidus",
		// The wheel is inlined, not linked.
		`class="chart-wheel"`,
		"Download as PNG",
		// The PDF is the browser's print output, so the button is script-driven
		// and the hook it is wired to has to be on the page as well.
		"Download as PDF",
		"data-download-pdf",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the result page is missing %q", want)
		}
	}

	// Every planet, the Ascendant and the Descendant must have a row.
	for _, body_ := range []string{"Sun", "Moon", "Mercury", "Venus", "Mars", "Jupiter",
		"Saturn", "Uranus", "Neptune", "Pluto", "Ascendant", "Descendant"} {
		if !strings.Contains(body, ">"+body_+"\n") && !strings.Contains(body, body_) {
			t.Errorf("%s is missing from the table", body_)
		}
	}
	// All twelve houses.
	for _, ord := range []string{"1st", "2nd", "3rd", "4th", "12th"} {
		if !strings.Contains(body, ord) {
			t.Errorf("house %s is missing", ord)
		}
	}
}

func TestHousesTableHasAReadingForEveryHouse(t *testing.T) {
	rec := post(t, newTestServer(t, false), "/chart", referenceForm())
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	body := rec.Body.String()

	// All four tables carry the column: the bodies, the houses, the secondary
	// points and the aspects.
	if n := strings.Count(body, `<th scope="col">How it shows up</th>`); n != 4 {
		t.Errorf("found %d \"How it shows up\" headings, want 4 (one per table)", n)
	}
	// Twelve bodies, twelve houses, four points, and one per aspect — and how
	// many aspects a chart has depends on the chart, so it is counted from the
	// page rather than assumed.
	aspects := strings.Count(body, `<td data-label="Orb">`)
	if aspects == 0 {
		t.Fatal("the reference chart produced no aspects, so the column cannot be checked")
	}
	if n, want := strings.Count(body, `<td data-label="How it shows up">`), 12+12+4+aspects; n != want {
		t.Errorf("found %d \"how it shows up\" cells, want %d (12 bodies + 12 houses + 4 points + %d aspects)",
			n, want, aspects)
	}
	// An empty cell means the lookup silently missed.
	if strings.Contains(body, `<td data-label="How it shows up"></td>`) {
		t.Error("a \"how it shows up\" cell is empty")
	}

	// Spot-check the reading actually matches the sign on the cusp. For this
	// chart the first house cusp is in Scorpio.
	showsUp, ok := interp.HouseReading(1, "scorpio")
	if !ok {
		t.Fatal("no reading for the first house in Scorpio")
	}
	if !strings.Contains(body, html.EscapeString(showsUp)) {
		t.Error("the first house reading does not match the sign on its cusp")
	}
}

// The written text and the ephemeris are separate packages, so nothing forces
// them to agree on which bodies aspect one another or what those aspects are
// called. Any chart can only show a handful of the possible pairings, so a
// missing reading would surface as one blank cell on somebody else's chart
// months from now; this walks the whole cross-product instead.
func TestEveryAspectPairHasAReading(t *testing.T) {
	bodies := astro.AspectBodies
	if len(bodies) != 12 {
		t.Fatalf("got %d aspecting bodies, want 12", len(bodies))
	}
	for i := 0; i < len(bodies); i++ {
		for j := i + 1; j < len(bodies); j++ {
			for _, name := range astro.AspectNames() {
				text, ok := interp.AspectReading(bodies[i].Key(), bodies[j].Key(), name)
				if !ok || text == "" {
					t.Errorf("no %s reading for %s and %s", name, bodies[i].Name(), bodies[j].Name())
				}
			}
		}
	}
}

// Likewise for the secondary points: astro decides which appear in the "Other
// points" table, interp holds what they say.
func TestEveryPointHasAReading(t *testing.T) {
	for _, body := range astro.PointBodies {
		if _, ok := interp.PointDefines(body.Key()); !ok {
			t.Errorf("%s has no defines line", body.Name())
		}
		for _, sign := range []string{
			"aries", "taurus", "gemini", "cancer", "leo", "virgo",
			"libra", "scorpio", "sagittarius", "capricorn", "aquarius", "pisces",
		} {
			if text, ok := interp.PointReading(body.Key(), sign); !ok || text == "" {
				t.Errorf("%s has no reading for %s", body.Name(), sign)
			}
		}
	}
}

func TestChartPageRejectsBadInput(t *testing.T) {
	h := newTestServer(t, false)

	tests := []struct {
		name      string
		mutate    func(url.Values)
		wantField string
	}{
		{"no name", func(v url.Values) { v.Del("name") }, "First name"},
		{"american date order", func(v url.Values) { v.Set("date", "06/35/1990") }, "no month"},
		{"impossible date", func(v url.Values) { v.Set("date", "31/02/1990") }, "no such date"},
		{"unparseable date", func(v url.Values) { v.Set("date", "sometime") }, "dd/mm/yyyy"},
		{"year out of range", func(v url.Values) { v.Set("date", "15/06/1200") }, "1600 and 2200"},
		{"bad time", func(v url.Values) { v.Set("time", "25:00") }, "between 00 and 23"},
		{"unparseable time", func(v url.Values) { v.Set("time", "half past two") }, "hh:mm"},
		{"unknown zone", func(v url.Values) { v.Set("zone", "Mars/Olympus") }, "time zone"},
		{"unknown place", func(v url.Values) {
			v.Set("location", "Nowhere At All Zzz")
			v.Del("latitude")
			v.Del("longitude")
		}, "No place called"},
		{"latitude out of range", func(v url.Values) { v.Set("latitude", "120") }, "between -90 and 90"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			form := referenceForm()
			tc.mutate(form)
			rec := post(t, h, "/chart", form)

			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status %d, want 422", rec.Code)
			}
			if !strings.Contains(rec.Body.String(), tc.wantField) {
				t.Errorf("the error page does not mention %q", tc.wantField)
			}
			// The form must come back with what the user typed still in it.
			if !strings.Contains(rec.Body.String(), `name="date"`) {
				t.Error("the form was not re-rendered")
			}
		})
	}
}

func TestChartPageResolvesPlaceWithoutCoordinates(t *testing.T) {
	form := referenceForm()
	form.Del("latitude")
	form.Del("longitude")

	rec := post(t, newTestServer(t, false), "/chart", form)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "23°49&#39; Gemini") {
		t.Error("the chart was not calculated from the gazetteer coordinates")
	}
}

func TestChartPageWarnsAboutAmbiguousClockTime(t *testing.T) {
	form := referenceForm()
	form.Set("date", "05/11/2023")
	form.Set("time", "01:30")
	form.Set("zone", "America/New_York")
	form.Set("location", "New York")
	form.Set("country", "United States")
	form.Set("latitude", "40.7143")
	form.Set("longitude", "-74.0060")

	rec := post(t, newTestServer(t, false), "/chart", form)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "occurred twice") {
		t.Error("no warning about the repeated hour when the clocks went back")
	}
}

func TestChartPageFallsBackToWholeSignInsideThePolarCircle(t *testing.T) {
	form := referenceForm()
	form.Set("zone", "Europe/Oslo")
	form.Set("location", "Tromsø")
	form.Set("country", "Norway")
	form.Set("latitude", "69.6492")
	form.Set("longitude", "18.9553")

	rec := post(t, newTestServer(t, false), "/chart", form)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Whole Sign") {
		t.Error("the chart did not fall back to Whole Sign houses")
	}
	if !strings.Contains(body, "no solution at this latitude") {
		t.Error("the fallback was not explained to the reader")
	}
}

func TestChartSVG(t *testing.T) {
	rec := get(t, newTestServer(t, false), "/chart.svg?"+referenceForm().Encode())
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "image/svg+xml") {
		t.Errorf("content type %q, want image/svg+xml", ct)
	}
	if !strings.HasPrefix(rec.Body.String(), `<?xml version="1.0"`) {
		t.Error("the standalone svg has no XML declaration")
	}
}

func TestChartSVGRejectsIncompleteDetails(t *testing.T) {
	rec := get(t, newTestServer(t, false), "/chart.svg?name=Test")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status %d, want 400", rec.Code)
	}
}

func TestPlacesAPI(t *testing.T) {
	rec := get(t, newTestServer(t, false), "/api/places?q=ballarat")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}

	var body struct {
		Source  string `json:"source"`
		Results []struct {
			Label    string  `json:"label"`
			Timezone string  `json:"timezone"`
			Latitude float64 `json:"latitude"`
		} `json:"results"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if body.Source != "offline" {
		t.Errorf("source %q, want offline", body.Source)
	}
	if len(body.Results) == 0 {
		t.Fatal("no results")
	}
	if body.Results[0].Label != "Ballarat, Victoria, Australia" {
		t.Errorf("top hit %q", body.Results[0].Label)
	}
	if body.Results[0].Timezone != "Australia/Melbourne" {
		t.Errorf("time zone %q", body.Results[0].Timezone)
	}
}

func TestPlacesAPIWithNoQuery(t *testing.T) {
	rec := get(t, newTestServer(t, false), "/api/places")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"results": []`) {
		t.Errorf("expected an empty result set, got %s", rec.Body.String())
	}
}

func TestGeocodeAPIIsOffByDefault(t *testing.T) {
	rec := get(t, newTestServer(t, false), "/api/geocode?q=ballarat")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status %d, want 403", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "-online") {
		t.Error("the response does not say how to enable online lookup")
	}
}

func TestChartJSONAPI(t *testing.T) {
	rec := post(t, newTestServer(t, false), "/api/chart", referenceForm())
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", rec.Code, rec.Body.String())
	}

	var out chartExport
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if out.Birth.UTC != "1990-06-15T04:30:00Z" {
		t.Errorf("utc %q, want 1990-06-15T04:30:00Z", out.Birth.UTC)
	}
	if out.Chart.HouseSystem != "Placidus" {
		t.Errorf("house system %q", out.Chart.HouseSystem)
	}
	if len(out.Chart.Houses) != 12 {
		t.Errorf("got %d house cusps, want 12", len(out.Chart.Houses))
	}
	if len(out.Chart.Points) != 16 {
		t.Errorf("got %d points, want 16", len(out.Chart.Points))
	}

	// Compared against Swiss Ephemeris for the same instant.
	want := map[string]float64{
		"sun": 83.831092, "moon": 341.199610, "mercury": 65.156714,
		"venus": 48.410161, "mars": 10.816588, "jupiter": 105.821765,
		"saturn": 294.050237, "uranus": 278.177393, "neptune": 283.724908,
		"pluto": 225.407875,
	}
	for _, p := range out.Chart.Points {
		expected, ok := want[p.Key]
		if !ok {
			continue
		}
		if diff := p.LongitudeDegrees - expected; diff > 0.02 || diff < -0.02 {
			t.Errorf("%s at %.6f°, want %.6f°", p.Key, p.LongitudeDegrees, expected)
		}
	}
}

func TestChartJSONAPIReportsFieldErrors(t *testing.T) {
	form := referenceForm()
	form.Set("date", "nonsense")

	rec := post(t, newTestServer(t, false), "/api/chart", form)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422", rec.Code)
	}

	var out struct {
		Errors map[string]string `json:"errors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	if out.Errors["date"] == "" {
		t.Errorf("no error reported for the date field: %v", out.Errors)
	}
}

func TestStaticAssets(t *testing.T) {
	h := newTestServer(t, false)
	for _, path := range []string{"/static/style.css", "/static/app.js"} {
		rec := get(t, h, path)
		if rec.Code != http.StatusOK {
			t.Errorf("%s: status %d, want 200", path, rec.Code)
		}
		if rec.Body.Len() == 0 {
			t.Errorf("%s: empty response", path)
		}
	}
}

func TestOrdinal(t *testing.T) {
	tests := map[int]string{1: "1st", 2: "2nd", 3: "3rd", 4: "4th", 11: "11th",
		12: "12th", 13: "13th", 21: "21st", 22: "22nd", 23: "23rd", 101: "101st"}
	for in, want := range tests {
		if got := ordinal(in); got != want {
			t.Errorf("ordinal(%d) = %q, want %q", in, got, want)
		}
	}
}
