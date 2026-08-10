package web

import (
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"astronomyCalculator/internal/astro"
	"astronomyCalculator/internal/places"
)

// Input is the birth form, as typed. It is kept as strings so a rejected
// submission can be handed straight back to the template with the user's own
// wording intact.
type Input struct {
	Name      string
	Date      string // dd/mm/yyyy
	Time      string // hh:mm
	Zone      string // IANA name
	Location  string
	Country   string
	Latitude  string
	Longitude string

	// TimeStatus and TimeSource say how well the time of birth is known and
	// where it came from. Both are optional and neither changes a single
	// computed position; they are carried so that a chart cast from a guessed
	// time cannot be mistaken for one cast from a certificate.
	TimeStatus string
	TimeSource string
}

// inputFromRequest reads the form or query string, whichever the request used.
func inputFromRequest(r *http.Request) Input {
	get := r.FormValue
	return Input{
		Name:       strings.TrimSpace(get("name")),
		Date:       strings.TrimSpace(get("date")),
		Time:       strings.TrimSpace(get("time")),
		Zone:       strings.TrimSpace(get("zone")),
		Location:   strings.TrimSpace(get("location")),
		Country:    strings.TrimSpace(get("country")),
		Latitude:   strings.TrimSpace(get("latitude")),
		Longitude:  strings.TrimSpace(get("longitude")),
		TimeStatus: strings.TrimSpace(get("timeStatus")),
		TimeSource: strings.TrimSpace(get("timeSource")),
	}
}

// Query re-encodes the input, so the result page can link to the standalone
// SVG and the JSON for the same chart.
func (in Input) Query() string {
	v := url.Values{}
	for key, value := range map[string]string{
		"name": in.Name, "date": in.Date, "time": in.Time, "zone": in.Zone,
		"location": in.Location, "country": in.Country,
		"latitude": in.Latitude, "longitude": in.Longitude,
		"timeStatus": in.TimeStatus, "timeSource": in.TimeSource,
	} {
		if value != "" {
			v.Set(key, value)
		}
	}
	return v.Encode()
}

// TimeStatusOptions are the choices offered for how well the birth time is
// known, each paired with the label the form shows.
type TimeStatusOption struct {
	Value    string
	Label    string
	Selected bool
}

// TimeStatusOptions builds the select for the form, marking whatever was chosen
// last time. "Not stated" comes first and is the default, because a form that
// defaulted to "recorded" would put words in the reader's mouth.
func (in Input) TimeStatusOptions() []TimeStatusOption {
	chosen := astro.ParseTimeStatus(in.TimeStatus)
	order := append([]astro.TimeStatus{astro.TimeUnstated}, astro.TimeStatuses()...)

	out := make([]TimeStatusOption, 0, len(order))
	seen := map[astro.TimeStatus]bool{}
	for _, status := range order {
		if seen[status] {
			continue
		}
		seen[status] = true
		out = append(out, TimeStatusOption{
			Value:    string(status),
			Label:    status.Label(),
			Selected: status == chosen,
		})
	}
	return out
}

// FieldErrors maps a form field name to the problem with it.
type FieldErrors map[string]string

func (e FieldErrors) add(field, msg string) {
	if _, exists := e[field]; !exists {
		e[field] = msg
	}
}

// Any reports whether anything failed validation.
func (e FieldErrors) Any() bool { return len(e) > 0 }

// resolve turns the typed form into the birth details a chart needs, filling
// in the coordinates from the gazetteer when the browser has not already done
// so. It also returns anything the reader should be told about how the input
// was interpreted.
func (in Input) resolve() (astro.Birth, []astro.Warning, FieldErrors) {
	errs := FieldErrors{}
	var birth astro.Birth
	var warnings []astro.Warning

	if in.Name == "" {
		errs.add("name", "Please enter a first name.")
	}
	birth.Name = in.Name

	year, month, day, err := parseDate(in.Date)
	if err != nil {
		errs.add("date", err.Error())
	}
	hour, minute, err := parseTime(in.Time)
	if err != nil {
		errs.add("time", err.Error())
	}

	place, err := in.resolvePlace()
	if err != nil {
		errs.add("location", err.Error())
	}

	// An explicit zone wins; then the place's own zone, which is what the picker
	// fills in; then, for coordinates typed by hand, the zone of the nearest
	// place the gazetteer does know.
	zoneName, inferredFrom := in.Zone, ""
	if zoneName == "" {
		zoneName = place.Timezone
	}
	if zoneName == "" && err == nil {
		zoneName, inferredFrom = zoneNear(place.Latitude, place.Longitude)
	}
	// Reported only when the place itself was understood. A place that could not
	// be resolved leaves nothing to take a zone from, and two errors raised by
	// one mistake send the reader hunting for a second problem there isn't.
	loc, zoneErr := places.LookupZone(zoneName)
	if zoneErr != nil && err == nil {
		errs.add("zone", "Please choose a time zone.")
	}

	if errs.Any() {
		return birth, nil, errs
	}

	birth.Year, birth.Month, birth.Day = year, month, day
	birth.Hour, birth.Minute = hour, minute
	birth.Zone, birth.ZoneName = loc, zoneName
	birth.Place = place.Name
	if in.Location != "" {
		birth.Place = in.Location
	}
	birth.Country = place.Country
	if in.Country != "" {
		birth.Country = in.Country
	}
	birth.Latitude, birth.Longitude = place.Latitude, place.Longitude
	birth.TimeStatus = astro.ParseTimeStatus(in.TimeStatus)
	birth.TimeSource = in.TimeSource

	if inferredFrom != "" {
		warnings = append(warnings, inferredZoneWarning(zoneName, inferredFrom, birthOffsetLabel(birth)))
	} else if w := checkZoneAgainstPlace(zoneName, birth.Latitude, birth.Longitude); w != nil {
		warnings = append(warnings, *w)
	}
	return birth, warnings, errs
}

// zoneNear returns the time zone of the nearest place in the gazetteer, and the
// name of that place.
//
// It is the fallback for coordinates typed by hand, which is what a reader is
// left with when the birthplace is too small to be listed. Those coordinates
// carry no zone, and until now the form refused to cast the chart at all: a
// point in Zambales is not plausibly in UTC, and making someone pick from six
// hundred zone names is a poor answer to a question the coordinates have
// already very nearly settled. Nearly, not quite — hence the warning that goes
// with it.
func zoneNear(lat, lon float64) (zone, nearest string) {
	zones, name, err := places.ZonesNear(lat, lon, 1)
	if err != nil || len(zones) == 0 {
		return "", ""
	}
	return zones[0], name
}

// inferredZoneWarning says which zone was assumed and where it came from. The
// assumption is sound in the middle of a country and can be an hour or more out
// near a border, and the reader is the only one in a position to tell which
// case this is.
func inferredZoneWarning(zoneName, nearest, offset string) astro.Warning {
	return astro.Warning{
		Code:     astro.WarnTimeZoneInferred,
		Severity: astro.SeverityWarning,
		Message: fmt.Sprintf(
			"No time zone was chosen, and these coordinates were entered by hand rather than picked from the list, so the chart was cast in %s (%s) — the zone of %s, the nearest place on file. "+
				"That is right unless the birthplace is close to a zone border. If it is, go back and choose the zone explicitly.",
			zoneName, offset, nearest),
		AffectedFields: []string{
			astro.FieldTimeZone, astro.FieldUTC, astro.FieldAngles, astro.FieldHouses,
		},
	}
}

// birthOffsetLabel renders the offset actually used for a birth: the one in
// force in its zone on its own date, not the one in force today.
//
// The difference is the whole point. Saying a 1990 Philippine birth was cast in
// "Asia/Manila (UTC+08:00)" when it was cast at +09:00 — the country kept
// daylight saving that winter — would be a warning that misreports the very
// thing it exists to disclose. ResolveLocal is the same call the chart makes,
// so the two cannot drift apart.
func birthOffsetLabel(b astro.Birth) string {
	local, _ := astro.ResolveLocal(b.Year, b.Month, b.Day, b.Hour, b.Minute, b.Zone)
	_, offset := local.Zone()
	return offsetText(offset)
}

// zoneNeighbourhood is how many nearby places are consulted when checking that
// a chosen time zone is plausible for a set of coordinates. Enough to span a
// zone border, few enough that a genuinely wrong zone still stands out.
const zoneNeighbourhood = 12

// checkZoneAgainstPlace reports when the chosen time zone does not belong to
// the part of the world the coordinates point at.
//
// This is the guard against the worst failure this application can have: a
// chart that is silently hours out because the zone was wrong. The hour of
// birth sets the Ascendant and every house cusp, so an error here is far more
// damaging than it looks, and nothing else on the page would reveal it.
func checkZoneAgainstPlace(zoneName string, lat, lon float64) *astro.Warning {
	zones, nearest, err := places.ZonesNear(lat, lon, zoneNeighbourhood)
	if err != nil || len(zones) == 0 {
		return nil // no gazetteer, nothing to compare against
	}
	if slices.Contains(zones, zoneName) {
		return nil
	}

	offset := zoneOffsetLabel(zoneName)
	expected := zoneOffsetLabel(zones[0])
	return &astro.Warning{
		Code:     astro.WarnTimeZoneMismatch,
		Severity: astro.SeverityWarning,
		Message: fmt.Sprintf(
			"The time of birth was read as %s (%s), but the nearest place to these coordinates is %s, which is in %s (%s). "+
				"If that is not what you meant, go back and choose the time zone of the birthplace — the chart will otherwise be out by the difference between them.",
			zoneName, offset, nearest, zones[0], expected),
		AffectedFields: []string{
			astro.FieldTimeZone, astro.FieldUTC, astro.FieldAngles, astro.FieldHouses,
		},
	}
}

// zoneOffsetLabel renders a zone's current offset, as a hint to the size of any
// mistake. The chart itself uses the offset in force on the date of birth.
func zoneOffsetLabel(name string) string {
	loc, err := places.LookupZone(name)
	if err != nil {
		return "unknown offset"
	}
	_, offset := time.Now().In(loc).Zone()
	return offsetText(offset)
}

// offsetText renders an offset in seconds as UTC±hh:mm.
func offsetText(seconds int) string {
	sign := "+"
	if seconds < 0 {
		sign, seconds = "-", -seconds
	}
	return fmt.Sprintf("UTC%s%02d:%02d", sign, seconds/3600, (seconds%3600)/60)
}

// resolvePlace prefers coordinates the picker has already filled in, and falls
// back to looking the typed text up in the gazetteer.
func (in Input) resolvePlace() (places.Place, error) {
	if in.Latitude != "" && in.Longitude != "" {
		lat, errLat := strconv.ParseFloat(in.Latitude, 64)
		lon, errLon := strconv.ParseFloat(in.Longitude, 64)
		switch {
		case errLat != nil || errLon != nil:
			return places.Place{}, fmt.Errorf("The coordinates for this place could not be read.")
		case lat < -90 || lat > 90:
			return places.Place{}, fmt.Errorf("Latitude must be between -90 and 90.")
		case lon < -180 || lon > 180:
			return places.Place{}, fmt.Errorf("Longitude must be between -180 and 180.")
		case in.Location == "":
			// The coordinates are enough to cast the chart and not enough to
			// label it. A chart headed by a blank is worse than one that asked.
			return places.Place{}, fmt.Errorf(
				"Please also name the place of birth. It need not be one on file — the name is kept as you type it.")
		}
		return places.Place{
			Name: in.Location, Country: in.Country,
			Latitude: lat, Longitude: lon, Timezone: in.Zone,
		}, nil
	}

	if in.Location == "" {
		return places.Place{}, fmt.Errorf("Please enter a place of birth.")
	}
	p, err := places.Resolve(in.Location, in.Country)
	if err != nil {
		// The way out has to be in the message. The gazetteer holds populated
		// places, not every hamlet, and a reader whose birthplace is missing was
		// previously told only that it did not exist.
		return places.Place{}, fmt.Errorf(
			"No place called %q was found. Try a nearby larger town, use the online search, "+
				"or open “Set the coordinates by hand” below and enter the latitude and longitude — "+
				"the name you have typed will be kept as it is.", in.Location)
	}
	return p, nil
}

// parseDate reads a day-first date. Slashes, dashes and dots are all accepted,
// because people type all three.
func parseDate(s string) (int, time.Month, int, error) {
	if s == "" {
		return 0, 0, 0, fmt.Errorf("Please enter a date of birth.")
	}
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return r == '/' || r == '-' || r == '.' || r == ' '
	})
	if len(fields) != 3 {
		return 0, 0, 0, fmt.Errorf("Please write the date as dd/mm/yyyy.")
	}

	day, err1 := strconv.Atoi(fields[0])
	month, err2 := strconv.Atoi(fields[1])
	year, err3 := strconv.Atoi(fields[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, 0, 0, fmt.Errorf("Please write the date as dd/mm/yyyy.")
	}
	if month < 1 || month > 12 {
		return 0, 0, 0, fmt.Errorf("There is no month %d.", month)
	}
	if year < 1600 || year > 2200 {
		return 0, 0, 0, fmt.Errorf("Please enter a four-digit year between 1600 and 2200.")
	}
	// Round-tripping catches the 31st of February without a table of month
	// lengths, leap years included.
	if t := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC); t.Day() != day ||
		int(t.Month()) != month || t.Year() != year {
		return 0, 0, 0, fmt.Errorf("There is no such date as %d/%d/%d.", day, month, year)
	}
	return year, time.Month(month), day, nil
}

// parseTime reads a 24-hour clock time.
func parseTime(s string) (int, int, error) {
	if s == "" {
		return 0, 0, fmt.Errorf("Please enter a time of birth. If it is unknown, 12:00 is the usual convention.")
	}
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("Please write the time as hh:mm on a 24-hour clock.")
	}
	hour, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	minute, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil {
		return 0, 0, fmt.Errorf("Please write the time as hh:mm on a 24-hour clock.")
	}
	if hour < 0 || hour > 23 {
		return 0, 0, fmt.Errorf("The hour must be between 00 and 23.")
	}
	if minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("The minutes must be between 00 and 59.")
	}
	return hour, minute, nil
}
