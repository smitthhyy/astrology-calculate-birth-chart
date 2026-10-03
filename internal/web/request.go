package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"reflect"
	"strconv"
	"strings"

	"astronomyCalculator/internal/astro"
)

// A request body is capped, because nothing legitimate comes close. The ten
// fields below are a few hundred bytes; the largest thing a caller might
// plausibly send is an export document fed back in, which runs to tens of
// kilobytes with its interpretations attached.
const maxRequestBody = 256 << 10

// chartRequest is the JSON body form of the birth details.
//
// The field names are the form's, so that the same details can be sent either
// way and a reader of one example can write the other. Everything is a string
// once parsed, because Input is: the export echoes the input back verbatim
// under rawInput, and a number reformatted on the way through would make the
// document a poor record of what was actually sent.
type chartRequest struct {
	Name     scalarString `json:"name"`
	Date     scalarString `json:"date"`
	Time     scalarString `json:"time"`
	Zone     scalarString `json:"zone"`
	Location scalarString `json:"location"`
	Country  scalarString `json:"country"`

	Latitude  scalarString `json:"latitude"`
	Longitude scalarString `json:"longitude"`
	// Coordinates is the pair written as one string, "15.061180, 120.069289",
	// which is how coordinates are usually quoted and copied. It fills Latitude
	// and Longitude when those were not given separately.
	Coordinates scalarString `json:"coordinates"`

	TimeStatus scalarString `json:"timeStatus"`
	TimeSource scalarString `json:"timeSource"`

	// MinorAspects is a pointer so that an absent field can be told from a
	// false one: absent leaves the query string free to decide, which is what
	// lets POST /api/chart?minorAspects=1 keep working with a JSON body.
	MinorAspects *bool `json:"minorAspects"`

	// RawInput is the export document's own record of the details it was cast
	// from. Accepting it means a saved chart can be posted straight back to
	// recast it, which is the round trip the export format promises. Anything
	// given at the top level wins over it.
	RawInput *chartRequest `json:"rawInput"`
}

// input turns the body into the form Input the rest of the package works with.
func (c chartRequest) input() Input {
	in := Input{
		Name:       c.Name.trimmed(),
		Date:       c.Date.trimmed(),
		Time:       c.Time.trimmed(),
		Zone:       c.Zone.trimmed(),
		Location:   c.Location.trimmed(),
		Country:    c.Country.trimmed(),
		Latitude:   c.Latitude.trimmed(),
		Longitude:  c.Longitude.trimmed(),
		TimeStatus: c.TimeStatus.trimmed(),
		TimeSource: c.TimeSource.trimmed(),
	}
	if in.Latitude == "" && in.Longitude == "" {
		in.Latitude, in.Longitude = splitCoordinates(c.Coordinates.trimmed())
	}
	if c.RawInput != nil {
		in = fillBlanks(in, c.RawInput.input())
	}
	return in
}

// fillBlanks takes each empty field of in from fallback. It is what makes the
// nested rawInput a default rather than an override.
func fillBlanks(in, fallback Input) Input {
	return Input{
		Name:       or(in.Name, fallback.Name),
		Date:       or(in.Date, fallback.Date),
		Time:       or(in.Time, fallback.Time),
		Zone:       or(in.Zone, fallback.Zone),
		Location:   or(in.Location, fallback.Location),
		Country:    or(in.Country, fallback.Country),
		Latitude:   or(in.Latitude, fallback.Latitude),
		Longitude:  or(in.Longitude, fallback.Longitude),
		TimeStatus: or(in.TimeStatus, fallback.TimeStatus),
		TimeSource: or(in.TimeSource, fallback.TimeSource),
	}
}

func or(preferred, fallback string) string {
	if preferred != "" {
		return preferred
	}
	return fallback
}

// splitCoordinates reads a latitude and longitude written as one string. A
// comma is the usual separator and whitespace alone is accepted too; anything
// else is left for resolvePlace to reject with a message about coordinates,
// which is the field the reader needs to look at.
func splitCoordinates(s string) (lat, lon string) {
	if s == "" {
		return "", ""
	}
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t'
	})
	if len(parts) != 2 {
		return s, "" // one field, so the error names latitude rather than nothing
	}
	return parts[0], parts[1]
}

// scalarString is a string that also accepts a JSON number or boolean.
//
// Coordinates are the reason. A caller writing the body by hand will quote them
// as strings; one serialising a struct will send `"latitude": 15.06118`. Both
// are obviously meant, and rejecting either would be a distinction the reader
// has to discover from a 400.
type scalarString string

func (s scalarString) trimmed() string { return strings.TrimSpace(string(s)) }

func (s *scalarString) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "null" {
		*s = ""
		return nil
	}
	if strings.HasPrefix(trimmed, `"`) {
		var text string
		if err := json.Unmarshal(data, &text); err != nil {
			return err
		}
		*s = scalarString(text)
		return nil
	}

	// A number is rendered with the shortest form that round-trips, so that
	// 15.06118 does not come back as 15.061180000000001 in rawInput.
	var number float64
	if err := json.Unmarshal(data, &number); err == nil {
		*s = scalarString(strconv.FormatFloat(number, 'f', -1, 64))
		return nil
	}
	var flag bool
	if err := json.Unmarshal(data, &flag); err == nil {
		*s = scalarString(strconv.FormatBool(flag))
		return nil
	}
	return fmt.Errorf("expected a string or a number, got %s", trimmed)
}

// apiInput reads the birth details from whichever way the caller sent them: a
// JSON body, a form, or the query string. The returned error means the request
// could not be read at all, as against details that were read and found
// wanting, which resolve() reports field by field.
func apiInput(w http.ResponseWriter, r *http.Request) (Input, astro.Options, error) {
	if !isJSONRequest(r) {
		// Unchanged for every caller that came before: r.FormValue covers both
		// a form-encoded body and the query string.
		return inputFromRequest(r), exportOptions(r), nil
	}

	// Decoded before anything touches r.FormValue, which would consume the
	// body and discard it as an unreadable form.
	var body chartRequest
	if err := decodeJSONBody(w, r, &body); err != nil {
		return Input{}, astro.Options{}, err
	}

	options := exportOptions(r)
	if body.MinorAspects != nil {
		options.MinorAspects = *body.MinorAspects
	} else if body.RawInput != nil && body.RawInput.MinorAspects != nil {
		options.MinorAspects = *body.RawInput.MinorAspects
	}
	return body.input(), options, nil
}

// isJSONRequest reports whether the body should be read as JSON. The +json
// suffix covers the structured types a client library might send.
func isJSONRequest(r *http.Request) bool {
	if r.Body == nil || r.Method == http.MethodGet || r.Method == http.MethodHead {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil {
		return false
	}
	return mediaType == "application/json" || mediaType == "text/json" ||
		strings.HasSuffix(mediaType, "+json")
}

// decodeJSONBody reads one JSON object from the body.
//
// Unknown fields are allowed rather than rejected. The export document declares
// itself feedable back in, and it carries a great deal more than these few
// keys; refusing it because it also has a calculatedChart would break the one
// round trip the format promises.
func decodeJSONBody(w http.ResponseWriter, r *http.Request, into *chartRequest) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)

	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(into); err != nil {
		return jsonBodyError(err)
	}
	// A second document in the same body means the caller sent something other
	// than what they think they sent, and casting the first would hide it.
	if err := dec.Decode(new(json.RawMessage)); err != io.EOF {
		return errors.New("The request body must hold one JSON object and nothing after it.")
	}
	return nil
}

// jsonBodyError turns a decoder failure into something a caller can act on.
// The standard library's own messages are precise but assume the reader knows
// Go's type names, so the common cases are reworded and the offset kept.
func jsonBodyError(err error) error {
	var syntax *json.SyntaxError
	var unmarshal *json.UnmarshalTypeError
	var tooLarge *http.MaxBytesError

	switch {
	case errors.Is(err, io.EOF):
		return errors.New("The request body was empty. Send the birth details as a JSON object.")
	case errors.As(err, &syntax):
		return fmt.Errorf("The request body is not valid JSON (at byte %d).", syntax.Offset)
	case errors.As(err, &unmarshal):
		return fmt.Errorf("The %q field could not be read as %s.", unmarshal.Field, wantedType(unmarshal))
	case errors.As(err, &tooLarge):
		return fmt.Errorf("The request body is larger than %d bytes.", maxRequestBody)
	case errors.Is(err, io.ErrUnexpectedEOF):
		return errors.New("The request body ended in the middle of the JSON.")
	default:
		return errors.New("The request body could not be read as JSON.")
	}
}

// wantedType names the type a field should have been, in the words the request
// is written in rather than Go's.
func wantedType(err *json.UnmarshalTypeError) string {
	if err.Type != nil && err.Type.Kind() == reflect.Bool {
		return "true or false"
	}
	return "text or a number"
}
