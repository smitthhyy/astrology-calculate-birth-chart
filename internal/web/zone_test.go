package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"
	_ "time/tzdata"

	"astronomyCalculator/internal/astro"
)

// The form must not pre-fill a time zone. It once defaulted to the server's own
// zone — which on Windows is an abbreviation rather than an IANA name, so it
// fell back to UTC — and because resolve() treated any non-empty zone as a
// deliberate choice, a birth in Adelaide was silently cast in UTC and came out
// nine and a half hours wrong.
func TestFormDoesNotPreFillATimeZone(t *testing.T) {
	body := get(t, newTestServer(t, false), "/").Body.String()
	if !strings.Contains(body, `name="zone" id="zone" value=""`) {
		t.Error("the zone field is pre-filled; the birthplace's own zone must win by default")
	}
	if !strings.Contains(body, "No time zone chosen") {
		t.Error("the form does not say that no zone has been chosen yet")
	}
}

func chartJSONFor(t *testing.T, form url.Values) chartExport {
	t.Helper()
	rec := post(t, newTestServer(t, false), "/api/chart", form)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var out chartExport
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding response: %v", err)
	}
	return out
}

// Adelaide observed daylight saving during the war and again from 1971, but
// not in between. Getting that wrong shifts the chart by an hour, so each of
// these years is pinned. Expected values are from Swiss Ephemeris 2.10.03.
func TestAdelaideDaylightSavingByYear(t *testing.T) {
	tests := []struct {
		name    string
		date    string
		wantUTC string
		wantDST bool
	}{
		{"February 1962, no daylight saving", "24/02/1962", "1962-02-24T01:34:00Z", false},
		{"July 1962, no daylight saving", "24/07/1962", "1962-07-24T01:34:00Z", false},
		{"February 1943, wartime daylight saving", "24/02/1943", "1943-02-24T00:34:00Z", true},
		{"February 1972, daylight saving reintroduced", "24/02/1972", "1972-02-24T00:34:00Z", true},
		{"July 1972, back on standard time", "24/07/1972", "1972-07-24T01:34:00Z", false},
		{"February 2024, daylight saving", "24/02/2024", "2024-02-24T00:34:00Z", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			form := url.Values{
				"name": {"T"}, "date": {tc.date}, "time": {"11:04"},
				"zone":     {"Australia/Adelaide"},
				"location": {"Adelaide"}, "country": {"Australia"},
				"latitude": {"-34.9287"}, "longitude": {"138.5986"},
			}
			out := chartJSONFor(t, form)
			if out.Birth.UTC != tc.wantUTC {
				t.Errorf("utc %s, want %s", out.Birth.UTC, tc.wantUTC)
			}
			// A +10:30 local offset means daylight saving was applied.
			gotDST := strings.Contains(out.Birth.Local, "+10:30")
			if gotDST != tc.wantDST {
				t.Errorf("local %s: daylight saving applied = %v, want %v", out.Birth.Local, gotDST, tc.wantDST)
			}
			if out.Birth.TimeZone.DaylightSaving != tc.wantDST {
				t.Errorf("timeZone.daylightSaving = %v, want %v",
					out.Birth.TimeZone.DaylightSaving, tc.wantDST)
			}
			for _, w := range out.Warnings {
				if w.Code == astro.WarnTimeZoneMismatch {
					t.Errorf("unexpected zone warning for a correct zone: %s", w.Message)
				}
			}
		})
	}
}

// The reported case, end to end: no zone supplied, so the birthplace's own zone
// has to be used. Checked against Swiss Ephemeris for 1962-02-24 01:34 UT.
func TestAdelaide1962UsesThePlacesZoneWhenNoneIsChosen(t *testing.T) {
	out := chartJSONFor(t, url.Values{
		"name": {"T"}, "date": {"24/02/1962"}, "time": {"11:04"},
		"location": {"Adelaide"}, "country": {"Australia"},
	})

	if out.Birth.TimeZone.Name != "Australia/Adelaide" {
		t.Fatalf("zone %q, want Australia/Adelaide", out.Birth.TimeZone.Name)
	}
	if out.Birth.UTC != "1962-02-24T01:34:00Z" {
		t.Errorf("utc %s, want 1962-02-24T01:34:00Z", out.Birth.UTC)
	}
	if !strings.HasSuffix(out.Birth.Local, "+09:30") {
		t.Errorf("local %s, want a +09:30 offset", out.Birth.Local)
	}
	if out.Birth.TimeZone.UTCOffset != "+09:30" {
		t.Errorf("timeZone.utcOffset %q, want +09:30", out.Birth.TimeZone.UTCOffset)
	}

	want := map[string]string{
		"sun": "4°57'", "moon": "23°56'", "ascendant": "7°48'", "midheaven": "13°05'",
	}
	signs := map[string]string{
		"sun": "Pisces", "moon": "Libra", "ascendant": "Taurus", "midheaven": "Aquarius",
	}
	for _, p := range out.Chart.Points {
		deg, ok := want[p.Key]
		if !ok {
			continue
		}
		// Compared to the arcminute: the reference figures are quoted that way,
		// and the arcsecond the document carries is finer than the agreement
		// between two ephemerides for the outer planets.
		got := fmt.Sprintf("%d°%02d'", p.Position.Degree, p.Position.Minute)
		if got != deg || p.Sign.Name != signs[p.Key] {
			t.Errorf("%s at %s %s, want %s %s", p.Key, got, p.Sign.Name, deg, signs[p.Key])
		}
	}
}

func TestZoneMismatchIsReported(t *testing.T) {
	tests := []struct {
		name     string
		zone     string
		lat, lon string
		wantWarn bool
		mentions string
	}{
		{
			name: "UTC chosen for an Australian birthplace",
			zone: "UTC", lat: "-34.9287", lon: "138.5986",
			wantWarn: true, mentions: "Australia/Adelaide",
		},
		{
			name: "the birthplace's own zone",
			zone: "Australia/Adelaide", lat: "-34.9287", lon: "138.5986",
		},
		{
			name: "a neighbouring zone that genuinely serves the area",
			// Broken Hill keeps South Australian time despite being in NSW.
			zone: "Australia/Adelaide", lat: "-31.95", lon: "141.4333",
		},
		{
			name: "a zone from the wrong hemisphere",
			zone: "America/New_York", lat: "-37.5662", lon: "143.8496",
			wantWarn: true, mentions: "Australia/Melbourne",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := chartJSONFor(t, url.Values{
				"name": {"T"}, "date": {"24/02/1962"}, "time": {"11:04"},
				"zone": {tc.zone}, "location": {"Somewhere"},
				"latitude": {tc.lat}, "longitude": {tc.lon},
			})

			var warning string
			for _, w := range out.Warnings {
				if w.Code == astro.WarnTimeZoneMismatch {
					warning = w.Message
				}
			}
			if tc.wantWarn && warning == "" {
				t.Fatalf("expected a zone warning, got %v", out.Warnings)
			}
			if !tc.wantWarn && warning != "" {
				t.Fatalf("unexpected zone warning: %s", warning)
			}
			if tc.mentions != "" && !strings.Contains(warning, tc.mentions) {
				t.Errorf("the warning does not name %s: %s", tc.mentions, warning)
			}
		})
	}
}

// The offset actually used has to be on the page. It is the one input a reader
// cannot check from the output, because an hour's error moves the angles a long
// way while leaving the planets looking almost right.
func TestResultPageStatesTheOffsetAndDaylightSaving(t *testing.T) {
	// Checked in pieces because html/template writes the plus of an offset as
	// &#43;, and pinning that escaping would be testing the template package.
	tests := []struct {
		name, date string
		want       []string
	}{
		{"standard time", "24/02/1962",
			[]string{"Australia/Adelaide", "09:30 (ACST)", "no daylight saving"}},
		{"daylight saving", "24/02/2024",
			[]string{"Australia/Adelaide", "10:30 (ACDT)", "daylight saving in force"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := post(t, newTestServer(t, false), "/chart", url.Values{
				"name": {"T"}, "date": {tc.date}, "time": {"11:04"},
				"zone": {"Australia/Adelaide"}, "location": {"Adelaide"},
				"country":  {"Australia"},
				"latitude": {"-34.9287"}, "longitude": {"138.5986"},
			})
			if rec.Code != http.StatusOK {
				t.Fatalf("status %d, want 200", rec.Code)
			}
			body := rec.Body.String()
			if !strings.Contains(body, "Time zone used") {
				t.Error("the result page does not label the time zone used")
			}
			for _, want := range tc.want {
				if !strings.Contains(body, want) {
					t.Errorf("the result page does not state %q", want)
				}
			}
		})
	}
}
