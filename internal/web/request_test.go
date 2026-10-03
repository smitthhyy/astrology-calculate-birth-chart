package web

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	_ "time/tzdata"
)

// The chart API takes the birth details as a JSON body as well as a form. The
// tests below use San Felipe in Zambales throughout, because it is the case the
// two paths have to agree on and the hardest one: the gazetteer does not hold
// the town, so every field the API would otherwise look up — the time zone, the
// country, the coordinates — has to come from the caller.

// sanFelipeJSON is that birth as a JSON body, with the coordinates as numbers.
const sanFelipeJSON = `{
  "name": "Test",
  "date": "15/06/1990",
  "time": "14:30",
  "zone": "Asia/Manila",
  "location": "San Felipe, Zambales",
  "country": "Philippines",
  "latitude": 15.061180,
  "longitude": 120.069289
}`

// sanFelipeForm is the same birth as the form fields, for comparing the paths.
func sanFelipeForm() url.Values {
	return url.Values{
		"name":      {"Test"},
		"date":      {"15/06/1990"},
		"time":      {"14:30"},
		"zone":      {"Asia/Manila"},
		"location":  {"San Felipe, Zambales"},
		"country":   {"Philippines"},
		"latitude":  {"15.06118"},
		"longitude": {"120.069289"},
	}
}

func postJSON(t *testing.T, h http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// decodeExport reads a successful response, failing the test with the body if
// the status was not 200 — an error document is far more use than "want 200".
func decodeExport(t *testing.T, rec *httptest.ResponseRecorder) chartExport {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var out chartExport
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	return out
}

// A JSON body is the obvious thing to send to something called an API, and
// until now it produced a 422 complaining that no name had been given: the
// handler read the form and the form was empty, because the details were in a
// body it never opened.
func TestChartAPIAcceptsAJSONBody(t *testing.T) {
	out := decodeExport(t, postJSON(t, newTestServer(t, false), "/api/chart", sanFelipeJSON))

	if out.Birth.Place.Name != "San Felipe, Zambales" {
		t.Errorf("place %q, want San Felipe, Zambales", out.Birth.Place.Name)
	}
	// The town is not on file, so the country cannot have been looked up. It is
	// here only because the caller sent it.
	if out.Birth.Place.Country != "Philippines" {
		t.Errorf("country %q, want Philippines", out.Birth.Place.Country)
	}
	if out.Birth.TimeZone.Name != "Asia/Manila" {
		t.Errorf("zone %q, want Asia/Manila", out.Birth.TimeZone.Name)
	}
	// +09:00, not the +08:00 the Philippines keeps today: it observed daylight
	// saving from 21 May to 28 July 1990, and this birth falls inside it.
	if out.Birth.TimeZone.UTCOffset != "+09:00" {
		t.Errorf("offset %q, want +09:00", out.Birth.TimeZone.UTCOffset)
	}
	if out.Birth.UTC != "1990-06-15T05:30:00Z" {
		t.Errorf("utc %q, want 1990-06-15T05:30:00Z", out.Birth.UTC)
	}
	if math.Abs(out.Birth.Place.Latitude-15.061180) > 1e-9 {
		t.Errorf("latitude %.9f, want 15.061180", out.Birth.Place.Latitude)
	}
	if math.Abs(out.Birth.Place.Longitude-120.069289) > 1e-9 {
		t.Errorf("longitude %.9f, want 120.069289", out.Birth.Place.Longitude)
	}
	if len(out.Chart.Points) != 16 {
		t.Errorf("got %d points, want 16", len(out.Chart.Points))
	}

	// The zone was chosen rather than assumed, so nothing should be warning
	// about an inferred one — that warning exists to flag a guess.
	for _, w := range out.Warnings {
		if w.Code == "time_zone_inferred" {
			t.Errorf("zone was given explicitly but reported as inferred: %s", w.Message)
		}
	}
}

// rawInput has to echo what was sent, because the export declares itself
// reproducible from it. A number written back as 15.061180000000001 would make
// that claim false in a way nobody would notice until they compared two files.
func TestJSONBodyIsEchoedInRawInput(t *testing.T) {
	out := decodeExport(t, postJSON(t, newTestServer(t, false), "/api/chart", sanFelipeJSON))

	for _, field := range []struct{ name, got, want string }{
		{"name", out.RawInput.Name, "Test"},
		{"date", out.RawInput.Date, "15/06/1990"},
		{"time", out.RawInput.Time, "14:30"},
		{"zone", out.RawInput.Zone, "Asia/Manila"},
		{"location", out.RawInput.Location, "San Felipe, Zambales"},
		{"country", out.RawInput.Country, "Philippines"},
		{"latitude", out.RawInput.Latitude, "15.06118"},
		{"longitude", out.RawInput.Longitude, "120.069289"},
	} {
		if field.got != field.want {
			t.Errorf("rawInput.%s = %q, want %q", field.name, field.got, field.want)
		}
	}
}

// A caller writing the body by hand quotes the coordinates; one serialising a
// struct sends numbers. Both are plainly meant, and the chart must not depend
// on which was chosen.
func TestJSONBodyTakesCoordinatesAsNumbersOrStrings(t *testing.T) {
	const asStrings = `{
	  "name": "Test", "date": "15/06/1990", "time": "14:30",
	  "zone": "Asia/Manila", "location": "San Felipe, Zambales",
	  "country": "Philippines",
	  "latitude": "15.061180", "longitude": "120.069289"
	}`
	// The pair as one string, which is how coordinates are usually quoted and
	// copied — "15.061180, 120.069289".
	const asOneString = `{
	  "name": "Test", "date": "15/06/1990", "time": "14:30",
	  "zone": "Asia/Manila", "location": "San Felipe, Zambales",
	  "country": "Philippines",
	  "coordinates": "15.061180, 120.069289"
	}`

	h := newTestServer(t, false)
	reference := decodeExport(t, postJSON(t, h, "/api/chart", sanFelipeJSON))

	for name, body := range map[string]string{
		"as strings":   asStrings,
		"as one field": asOneString,
	} {
		t.Run(name, func(t *testing.T) {
			out := decodeExport(t, postJSON(t, h, "/api/chart", body))
			if out.Birth.Place.Latitude != reference.Birth.Place.Latitude ||
				out.Birth.Place.Longitude != reference.Birth.Place.Longitude {
				t.Errorf("coordinates %v, %v, want %v, %v",
					out.Birth.Place.Latitude, out.Birth.Place.Longitude,
					reference.Birth.Place.Latitude, reference.Birth.Place.Longitude)
			}
			if out.Birth.UTC != reference.Birth.UTC {
				t.Errorf("utc %q, want %q", out.Birth.UTC, reference.Birth.UTC)
			}
		})
	}
}

// The two ways in must not drift apart. Every position is compared rather than
// a sample, because a difference in one body is exactly the kind of thing a
// spot check misses.
func TestJSONBodyAndFormAgree(t *testing.T) {
	h := newTestServer(t, false)
	fromJSON := decodeExport(t, postJSON(t, h, "/api/chart", sanFelipeJSON))
	fromForm := decodeExport(t, post(t, h, "/api/chart", sanFelipeForm()))

	if fromJSON.Birth.UTC != fromForm.Birth.UTC {
		t.Fatalf("utc %q from JSON, %q from the form", fromJSON.Birth.UTC, fromForm.Birth.UTC)
	}
	if len(fromJSON.Chart.Points) != len(fromForm.Chart.Points) {
		t.Fatalf("%d points from JSON, %d from the form",
			len(fromJSON.Chart.Points), len(fromForm.Chart.Points))
	}
	for i, p := range fromJSON.Chart.Points {
		other := fromForm.Chart.Points[i]
		if p.Key != other.Key {
			t.Fatalf("point %d is %q from JSON and %q from the form", i, p.Key, other.Key)
		}
		if p.LongitudeDegrees != other.LongitudeDegrees {
			t.Errorf("%s at %.9f° from JSON, %.9f° from the form",
				p.Key, p.LongitudeDegrees, other.LongitudeDegrees)
		}
	}
	if len(fromJSON.Chart.Aspects) != len(fromForm.Chart.Aspects) {
		t.Errorf("%d aspects from JSON, %d from the form",
			len(fromJSON.Chart.Aspects), len(fromForm.Chart.Aspects))
	}
}

// A body that cannot be read is not the same as details that can be read and
// are wrong, and the two need different statuses: 400 says fix the request, 422
// says fix the birth. Collapsing them would send a caller looking for a bad
// date in a body that never parsed.
func TestMalformedJSONBodyIsRejected(t *testing.T) {
	cases := map[string]string{
		"truncated":       `{"name": "Test", "date":`,
		"not an object":   `"just a string"`,
		"empty body":      ``,
		"two documents":   `{"name": "Test"} {"name": "Other"}`,
		"wrong type":      `{"name": {"first": "Test"}}`,
		"minorAspects":    `{"name": "Test", "minorAspects": "perhaps"}`,
		"array not given": `[{"name": "Test"}]`,
	}

	h := newTestServer(t, false)
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			rec := postJSON(t, h, "/api/chart", body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status %d, want 400: %s", rec.Code, rec.Body.String())
			}
			var out struct {
				Error string `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
				t.Fatalf("decoding response: %v", err)
			}
			if out.Error == "" {
				t.Errorf("no error message: %s", rec.Body.String())
			}
		})
	}
}

// Details that parse but do not validate keep the behaviour the form-encoded
// endpoint already has: 422, with the field named.
func TestJSONBodyReportsFieldErrors(t *testing.T) {
	const body = `{
	  "name": "Test", "date": "nonsense", "time": "14:30",
	  "zone": "Asia/Manila", "location": "San Felipe, Zambales",
	  "country": "Philippines",
	  "latitude": 15.061180, "longitude": 120.069289
	}`

	rec := postJSON(t, newTestServer(t, false), "/api/chart", body)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422: %s", rec.Code, rec.Body.String())
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

// The place still has to be named, whichever way it arrives. Coordinates alone
// are enough to cast the chart and not enough to label it.
func TestJSONBodyStillRequiresTheDetails(t *testing.T) {
	const body = `{
	  "name": "Test", "date": "15/06/1990", "time": "14:30",
	  "zone": "Asia/Manila",
	  "latitude": 15.061180, "longitude": 120.069289
	}`

	rec := postJSON(t, newTestServer(t, false), "/api/chart", body)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "location") {
		t.Errorf("the missing place was not reported: %s", rec.Body.String())
	}
}

// minorAspects is a calculation choice, so it belongs in the body as well as
// the query string. The query string keeps working with a JSON body, since a
// caller may well have the flag in the URL already.
func TestJSONBodyCarriesTheMinorAspectsFlag(t *testing.T) {
	body := strings.Replace(sanFelipeJSON, `"name": "Test",`,
		`"name": "Test", "minorAspects": true,`, 1)

	h := newTestServer(t, false)
	plain := decodeExport(t, postJSON(t, h, "/api/chart", sanFelipeJSON))
	fromBody := decodeExport(t, postJSON(t, h, "/api/chart", body))
	fromQuery := decodeExport(t, postJSON(t, h, "/api/chart?minorAspects=1", sanFelipeJSON))

	if !fromBody.Settings.AspectCalculation.IncludeMinorAspects {
		t.Error("minorAspects in the body was not honoured")
	}
	if !fromQuery.Settings.AspectCalculation.IncludeMinorAspects {
		t.Error("minorAspects in the query string was not honoured alongside a JSON body")
	}
	if plain.Settings.AspectCalculation.IncludeMinorAspects {
		t.Error("minor aspects are on by default")
	}
	if len(fromBody.Chart.Aspects) <= len(plain.Chart.Aspects) {
		t.Errorf("%d aspects with the minor ones, %d without",
			len(fromBody.Chart.Aspects), len(plain.Chart.Aspects))
	}
}

// The export says it can be fed back in to reproduce itself. Over the API that
// means posting the saved document as-is, which is only possible because the
// decoder ignores the hundred keys it does not know and reads rawInput.
func TestAnExportPostedBackReproducesItself(t *testing.T) {
	h := newTestServer(t, false)

	first := postJSON(t, h, "/api/chart", sanFelipeJSON)
	original := decodeExport(t, first)
	again := decodeExport(t, postJSON(t, h, "/api/chart", first.Body.String()))

	if again.Birth.UTC != original.Birth.UTC {
		t.Fatalf("utc %q, want %q", again.Birth.UTC, original.Birth.UTC)
	}
	if again.Birth.Place.Name != original.Birth.Place.Name {
		t.Errorf("place %q, want %q", again.Birth.Place.Name, original.Birth.Place.Name)
	}
	if again.Birth.TimeZone.Name != original.Birth.TimeZone.Name {
		t.Errorf("zone %q, want %q", again.Birth.TimeZone.Name, original.Birth.TimeZone.Name)
	}
	for i, p := range again.Chart.Points {
		if p.LongitudeDegrees != original.Chart.Points[i].LongitudeDegrees {
			t.Errorf("%s at %.9f°, want %.9f°",
				p.Key, p.LongitudeDegrees, original.Chart.Points[i].LongitudeDegrees)
		}
	}
	// Everything but generatedAt, which is when the file was written rather
	// than a property of the chart.
	again.GeneratedAt = original.GeneratedAt
	wantJSON, _ := json.Marshal(original)
	gotJSON, _ := json.Marshal(again)
	if string(gotJSON) != string(wantJSON) {
		t.Error("the recast document differs from the one it was built from")
	}
}

// A form-encoded or query-string caller must not notice any of this. The
// content-type branch is the only new thing in their path.
func TestFormAndQueryCallersAreUnaffected(t *testing.T) {
	h := newTestServer(t, false)

	fromForm := decodeExport(t, post(t, h, "/api/chart", referenceForm()))
	fromQuery := decodeExport(t, get(t, h, "/api/chart?"+referenceForm().Encode()))

	if fromForm.Birth.UTC != "1990-06-15T04:30:00Z" {
		t.Errorf("utc %q from the form, want 1990-06-15T04:30:00Z", fromForm.Birth.UTC)
	}
	if fromQuery.Birth.UTC != fromForm.Birth.UTC {
		t.Errorf("utc %q from the query string, %q from the form",
			fromQuery.Birth.UTC, fromForm.Birth.UTC)
	}
}

// The download names its file from the details as parsed, so a JSON body gets
// the same name a form would rather than an unnamed fallback.
func TestChartDownloadNamesTheFileFromTheParsedDetails(t *testing.T) {
	rec := get(t, newTestServer(t, false), "/chart.json?"+sanFelipeForm().Encode())
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="test-birth-chart.json"` {
		t.Errorf("Content-Disposition %q", got)
	}
}

func TestSplitCoordinates(t *testing.T) {
	cases := []struct{ in, lat, lon string }{
		{"15.061180, 120.069289", "15.061180", "120.069289"},
		{"15.061180,120.069289", "15.061180", "120.069289"},
		{"15.06118 120.069289", "15.06118", "120.069289"},
		{"-37.5662, 143.8496", "-37.5662", "143.8496"},
		{"", "", ""},
		// One value is not a pair. It is returned as the latitude so that the
		// error names a field the reader can go and look at.
		{"15.06118", "15.06118", ""},
		{"15, 120, 7", "15, 120, 7", ""},
	}
	for _, c := range cases {
		lat, lon := splitCoordinates(c.in)
		if lat != c.lat || lon != c.lon {
			t.Errorf("splitCoordinates(%q) = %q, %q; want %q, %q", c.in, lat, lon, c.lat, c.lon)
		}
	}
}

func TestIsJSONRequest(t *testing.T) {
	cases := map[string]bool{
		"application/json":                  true,
		"application/json; charset=utf-8":   true,
		"application/vnd.chart+json":        true,
		"text/json":                         true,
		"application/x-www-form-urlencoded": false,
		"text/plain":                        false,
		"":                                  false,
		"nonsense/;":                        false,
	}
	for contentType, want := range cases {
		req := httptest.NewRequest(http.MethodPost, "/api/chart", strings.NewReader("{}"))
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		if got := isJSONRequest(req); got != want {
			t.Errorf("isJSONRequest(%q) = %v, want %v", contentType, got, want)
		}
	}

	// A GET carries its details in the query string, whatever it claims about
	// a body it does not have.
	req := httptest.NewRequest(http.MethodGet, "/api/chart", nil)
	req.Header.Set("Content-Type", "application/json")
	if isJSONRequest(req) {
		t.Error("a GET was treated as having a JSON body")
	}
}

// An oversized body is refused rather than read into memory.
func TestOversizedJSONBodyIsRejected(t *testing.T) {
	body := `{"name": "` + strings.Repeat("x", maxRequestBody+1) + `"}`
	rec := postJSON(t, newTestServer(t, false), "/api/chart", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", rec.Code, rec.Body.String())
	}
}
