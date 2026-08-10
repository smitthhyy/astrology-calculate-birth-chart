package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"astronomyCalculator/internal/astro"
)

// The gazetteer holds populated places, not every hamlet, so a birthplace that
// is not in it has to be enterable by hand. That path was broken in three
// separate ways at once, and the tests below pin each of them.
//
// The reported case was San Felipe in Zambales, Philippines. Typing the
// address failed because everything after the first comma was matched as one
// string and the province matches no field the gazetteer holds. Entering the
// coordinates instead failed because no zone came with them and the form
// insisted on one. And doing both failed because the script cleared the
// coordinate boxes on every keystroke in the place field.

// coordinatesForm is a birth given entirely by hand: a place the gazetteer does
// not know, its coordinates typed in, and no time zone chosen.
func coordinatesForm() url.Values {
	return url.Values{
		"name":      {"Test"},
		"date":      {"15/06/1990"},
		"time":      {"14:30"},
		"location":  {"San Felipe, Zambales"},
		"country":   {"Philippines"},
		"latitude":  {"15.0619"},
		"longitude": {"120.0742"},
	}
}

// Coordinates typed by hand are enough on their own. Refusing to cast the chart
// until a zone is also chosen made the manual path useless: the coordinates
// have already all but answered the question.
func TestHandEnteredCoordinatesNeedNoTimeZone(t *testing.T) {
	out := chartJSONFor(t, coordinatesForm())

	if out.Birth.TimeZone.Name != "Asia/Manila" {
		t.Errorf("zone %q, want Asia/Manila", out.Birth.TimeZone.Name)
	}
	// +09:00, not the +08:00 the Philippines keeps today: it observed daylight
	// saving from 21 May to 28 July 1990, and this birth falls inside it.
	if out.Birth.TimeZone.UTCOffset != "+09:00" {
		t.Errorf("offset %q, want +09:00", out.Birth.TimeZone.UTCOffset)
	}
	if !out.Birth.TimeZone.DaylightSaving {
		t.Error("daylight saving was in force in the Philippines on this date")
	}
	if out.Birth.UTC != "1990-06-15T05:30:00Z" {
		t.Errorf("utc %s, want 1990-06-15T05:30:00Z", out.Birth.UTC)
	}
}

// An assumed zone has to say so. It is sound in the middle of a country and can
// be an hour or more out near a border, and only the reader knows which.
func TestAnInferredTimeZoneIsDeclared(t *testing.T) {
	out := chartJSONFor(t, coordinatesForm())

	var found astro.Warning
	for _, w := range out.Warnings {
		if w.Code == astro.WarnTimeZoneInferred {
			found = astro.Warning{Code: w.Code, Severity: w.Severity, Message: w.Message}
		}
	}
	if found.Code == "" {
		t.Fatalf("no inferred-zone warning: %+v", out.Warnings)
	}
	if found.Severity != astro.SeverityWarning {
		t.Errorf("severity %q, want %q", found.Severity, astro.SeverityWarning)
	}
	// The zone and the place it was taken from both have to be in the sentence,
	// because between them they are the whole of the assumption. The offset has
	// to be the one in force on the date of birth, not today's: the Philippines
	// kept daylight saving in mid-1990 and keeps none now, so a warning quoting
	// UTC+08:00 would misreport the very thing it exists to disclose.
	for _, want := range []string{"Asia/Manila", "UTC+09:00", "Philippines"} {
		if !strings.Contains(found.Message, want) {
			t.Errorf("the warning does not mention %q: %s", want, found.Message)
		}
	}
}

// One mistake, one message. Coordinates with no place name used to raise the
// name error and a second complaint that no time zone had been chosen — which
// was true, unfixable on its own, and not the problem.
func TestCoordinatesWithoutANameRaiseOneError(t *testing.T) {
	form := coordinatesForm()
	form.Del("location")

	rec := post(t, newTestServer(t, false), "/chart", form)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "name the place of birth") {
		t.Error("the reader is not asked to name the place")
	}
	if strings.Contains(body, "Please choose a time zone") {
		t.Error("a second error about the time zone was raised for the same mistake")
	}
}

// A zone that was chosen is never overridden, and gets no such warning.
func TestAChosenTimeZoneIsNotInferred(t *testing.T) {
	form := coordinatesForm()
	form.Set("zone", "Asia/Manila")
	out := chartJSONFor(t, form)

	for _, w := range out.Warnings {
		if w.Code == astro.WarnTimeZoneInferred {
			t.Errorf("a chosen zone was reported as inferred: %s", w.Message)
		}
	}
}

// The name and country are the reader's, not the gazetteer's. A place too small
// to be listed still has a name, and the chart is about that place.
func TestHandEnteredCoordinatesKeepTheTypedPlaceName(t *testing.T) {
	out := chartJSONFor(t, coordinatesForm())

	if out.Birth.Place.Name != "San Felipe, Zambales" {
		t.Errorf("place %q, want the name as typed", out.Birth.Place.Name)
	}
	if out.Birth.Place.Country != "Philippines" {
		t.Errorf("country %q, want Philippines", out.Birth.Place.Country)
	}
	if out.Birth.Place.Latitude != 15.0619 || out.Birth.Place.Longitude != 120.0742 {
		t.Errorf("coordinates %.4f, %.4f, want the ones typed",
			out.Birth.Place.Latitude, out.Birth.Place.Longitude)
	}
}

// The full address, province and all, now finds the town without any
// coordinates being given.
func TestATypedAddressWithAProvinceResolves(t *testing.T) {
	form := coordinatesForm()
	form.Del("latitude")
	form.Del("longitude")
	out := chartJSONFor(t, form)

	if out.Birth.TimeZone.Name != "Asia/Manila" {
		t.Errorf("zone %q, want Asia/Manila", out.Birth.TimeZone.Name)
	}
	// Resolved from the gazetteer, so no zone was inferred from coordinates.
	for _, w := range out.Warnings {
		if w.Code == astro.WarnTimeZoneInferred {
			t.Errorf("a resolved place should carry its own zone: %s", w.Message)
		}
	}
}

// A reader told their birthplace does not exist needs to be told what to do
// about it, in the same sentence.
func TestTheUnknownPlaceErrorOffersTheWayOut(t *testing.T) {
	form := referenceForm()
	form.Set("location", "Nowhere At All Zzz")
	form.Del("latitude")
	form.Del("longitude")

	rec := post(t, newTestServer(t, false), "/chart", form)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status %d, want 422", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "coordinates by hand") {
		t.Error("the error does not point at the manual coordinates")
	}
	// And the panel holding them is open, so it does not have to be hunted for.
	if !strings.Contains(body, `<details class="advanced" open>`) {
		t.Error("the coordinates panel is not opened when the place was not found")
	}
}

// The script clears the coordinate boxes when the place is retyped, so that
// coordinates belonging to a previously picked place cannot be attached to a
// different one. It must not clear coordinates the reader typed: doing so
// silently discarded the answer to the place not being on file, and the form
// came back saying the place did not exist. There is no JavaScript test
// harness here, so this checks the guard is present rather than its effect.
func TestTheScriptOnlyClearsCoordinatesItFilledIn(t *testing.T) {
	rec := get(t, newTestServer(t, false), "/static/app.js")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	script := rec.Body.String()

	if !strings.Contains(script, "coordsFromPicker") {
		t.Fatal("the script does not distinguish picked coordinates from typed ones")
	}
	// The clearing must sit behind the guard, not before it.
	clear := strings.Index(script, "if (latitude) latitude.value = '';")
	guard := strings.Index(script, "if (!coordsFromPicker) return;")
	if clear < 0 || guard < 0 || guard > clear {
		t.Error("the coordinates are cleared before the guard that protects typed ones")
	}
}
