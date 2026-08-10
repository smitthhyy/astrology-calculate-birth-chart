package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"astronomyCalculator/internal/astro"
	"astronomyCalculator/internal/places"
)

const searchLimit = 8

// handlePlaces searches the embedded gazetteer. Nothing leaves the machine.
func (s *Server) handlePlaces(w http.ResponseWriter, r *http.Request) {
	q, err := searchTerm(r)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"results": []places.Place{}})
		return
	}
	results, err := places.Search(q, searchLimit)
	if err != nil {
		s.logf("place search: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "The place list could not be read."})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results, "source": "offline"})
}

// handleGeocode looks a place up over the network. It is disabled unless the
// operator opted in, since it necessarily discloses the search term.
func (s *Server) handleGeocode(w http.ResponseWriter, r *http.Request) {
	if !s.allowOnline {
		writeJSON(w, http.StatusForbidden, map[string]any{
			"error": "Online lookup is switched off. Start the server with -online to enable it.",
		})
		return
	}
	q, err := searchTerm(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "Type a place name first."})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	results, err := places.Geocode(ctx, q, searchLimit)
	if err != nil {
		s.logf("online geocode: %v", err)
		writeJSON(w, http.StatusBadGateway, map[string]any{
			"error": "The online place search could not be reached.",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"results": results, "source": "online"})
}

// handleChartJSON returns the chart as JSON for a caller that will read it
// straight away. handleChartDownload serves the same document as a file.
func (s *Server) handleChartJSON(w http.ResponseWriter, r *http.Request) {
	s.serveChartJSON(w, r, "")
}

func (s *Server) handleChartDownload(w http.ResponseWriter, r *http.Request) {
	s.serveChartJSON(w, r, exportFilename(strings.TrimSpace(r.FormValue("name"))))
}

// serveChartJSON casts the chart and writes the export document. A non-empty
// filename turns the response into a download.
func (s *Server) serveChartJSON(w http.ResponseWriter, r *http.Request, filename string) {
	in := inputFromRequest(r)

	birth, warnings, errs := in.resolve()
	if errs.Any() {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"errors": errs})
		return
	}
	chart, err := astro.ComputeWith(birth, exportOptions(r))
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{"error": err.Error()})
		return
	}
	chart.Warnings = append(warnings, chart.Warnings...)

	if filename != "" {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	}
	writeJSON(w, http.StatusOK, buildExport(in, chart))
}

// exportOptions reads the calculation choices a JSON caller is allowed to make.
//
// Only the JSON endpoints offer these. The minor aspects have no written reading
// behind them, so switching them on would give the HTML page a table of rows
// with an empty column; a program consuming the data has no such problem, and
// the aspects themselves are standard enough to be worth offering. They are off
// by default because a chart with eleven aspect types in it is not what most
// astrologers mean by a chart.
func exportOptions(r *http.Request) astro.Options {
	return astro.Options{MinorAspects: boolParam(r, "minorAspects")}
}

// boolParam reads a query flag, accepting the several spellings a caller might
// reasonably use. A bare `?minorAspects` counts as true.
func boolParam(r *http.Request, name string) bool {
	if _, present := r.URL.Query()[name]; !present {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(r.FormValue(name))) {
	case "", "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// handleSchema serves the JSON Schema the export declares itself against, so
// that a consumer can validate a document without fetching anything from the
// network. It is served from the same origin as the data for the same reason
// everything else here is offline.
func (s *Server) handleSchema(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/schema+json; charset=utf-8")
	// The schema changes only when the program does, so it may be cached hard.
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(schemaJSON)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(body)
}
