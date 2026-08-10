// Package web serves the birth-chart form and results.
package web

import (
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"strings"

	"astronomyCalculator/internal/astro"
	"astronomyCalculator/internal/interp"
	"astronomyCalculator/internal/places"
	"astronomyCalculator/internal/wheel"
)

//go:embed templates/*.gohtml static/*
var assets embed.FS

// schemaJSON is the JSON Schema for the exported chart, embedded so that the
// document a caller receives and the schema it declares itself against always
// come from the same build.
//
//go:embed birth-chart.schema.json
var schemaJSON []byte

// Server holds everything the handlers share.
type Server struct {
	// pages holds one template set per page. Each set is layout plus that
	// page's own file, so both pages can define "main" and "title" without
	// colliding.
	pages map[string]*template.Template
	zones []places.Zone
	// allowOnline enables the opt-in geocoding endpoint. It is off unless
	// the operator turns it on, because it sends the typed place name to a
	// third party.
	allowOnline bool
	logger      *log.Logger
}

// New builds the server. It validates the embedded interpretation text up
// front so a bad data file fails at startup rather than blanking a column on
// somebody's chart.
func New(allowOnline bool, logger *log.Logger) (*Server, error) {
	if err := interp.Validate(); err != nil {
		return nil, err
	}
	if _, err := places.Count(); err != nil {
		return nil, err
	}

	pages := make(map[string]*template.Template)
	for _, name := range []string{"index.gohtml", "result.gohtml"} {
		t, err := template.New(name).Funcs(templateFuncs).
			ParseFS(assets, "templates/layout.gohtml", "templates/"+name)
		if err != nil {
			return nil, fmt.Errorf("web: parsing %s: %w", name, err)
		}
		pages[name] = t
	}

	return &Server{
		pages:       pages,
		zones:       places.Zones(),
		allowOnline: allowOnline,
		logger:      logger,
	}, nil
}

var templateFuncs = template.FuncMap{
	"ordinal": ordinal,
}

// ordinal renders a house number as 1st, 2nd, 3rd and so on.
func ordinal(n int) string {
	suffix := "th"
	if n%100 < 11 || n%100 > 13 {
		switch n % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return fmt.Sprintf("%d%s", n, suffix)
}

// Handler returns the router with every route registered.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	static, err := fs.Sub(assets, "static")
	if err != nil {
		panic(err) // the directory is embedded, so this cannot fail at runtime
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", cacheForever(http.FileServer(http.FS(static)))))

	mux.HandleFunc("GET /{$}", s.handleForm)
	mux.HandleFunc("POST /chart", s.handleChart)
	mux.HandleFunc("GET /chart", s.handleChart)
	mux.HandleFunc("GET /chart.svg", s.handleChartSVG)
	// chart.json is the same document as /api/chart, served as a download so
	// the result page can offer it as a link with no JavaScript involved.
	mux.HandleFunc("GET /chart.json", s.handleChartDownload)
	// The schema every exported document points at, served here so that
	// validating one needs no network either.
	mux.HandleFunc("GET "+schemaPath, s.handleSchema)
	mux.HandleFunc("GET /api/places", s.handlePlaces)
	mux.HandleFunc("GET /api/geocode", s.handleGeocode)
	mux.HandleFunc("GET /api/chart", s.handleChartJSON)
	mux.HandleFunc("POST /api/chart", s.handleChartJSON)

	return mux
}

func cacheForever(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		h.ServeHTTP(w, r)
	})
}

// formPage is the data the entry form needs.
type formPage struct {
	Input       Input
	Errors      FieldErrors
	Zones       []places.Zone
	AllowOnline bool
	PlaceCount  int
}

func (s *Server) handleForm(w http.ResponseWriter, r *http.Request) {
	// The zone is deliberately left blank rather than pre-filled. It belongs to
	// the birthplace, not to whoever is running the server, and a pre-filled
	// value would be indistinguishable from a deliberate choice — which is
	// exactly how a birth in Adelaide once got cast in UTC.
	s.renderForm(w, http.StatusOK, inputFromRequest(r), nil)
}

func (s *Server) renderForm(w http.ResponseWriter, status int, in Input, errs FieldErrors) {
	count, _ := places.Count()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	s.render(w, "index.gohtml", formPage{
		Input:       in,
		Errors:      errs,
		Zones:       s.zones,
		AllowOnline: s.allowOnline,
		PlaceCount:  count,
	})
}

func (s *Server) handleChart(w http.ResponseWriter, r *http.Request) {
	in := inputFromRequest(r)

	birth, warnings, errs := in.resolve()
	if errs.Any() {
		s.renderForm(w, http.StatusUnprocessableEntity, in, errs)
		return
	}

	chart, err := astro.Compute(birth)
	if err != nil {
		s.renderForm(w, http.StatusUnprocessableEntity, in, FieldErrors{
			"date": "This chart could not be calculated: " + err.Error(),
		})
		return
	}
	chart.Warnings = append(warnings, chart.Warnings...)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.render(w, "result.gohtml", buildResult(in, chart))
}

func (s *Server) handleChartSVG(w http.ResponseWriter, r *http.Request) {
	in := inputFromRequest(r)

	birth, _, errs := in.resolve()
	if errs.Any() {
		http.Error(w, "The chart details are incomplete.", http.StatusBadRequest)
		return
	}
	chart, err := astro.Compute(birth)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}

	svg := wheel.Render(chart, wheel.Options{
		Title:      chart.Birth.Name,
		Subtitle:   chart.Local.Format("2 January 2006, 15:04") + " · " + chart.Birth.Place,
		Standalone: true,
	})

	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.Header().Set("Content-Disposition", `inline; filename="birth-chart.svg"`)
	if _, err := w.Write([]byte(svg)); err != nil {
		s.logf("writing svg: %v", err)
	}
}

func (s *Server) render(w http.ResponseWriter, name string, data any) {
	page, ok := s.pages[name]
	if !ok {
		s.logf("no such page template: %s", name)
		return
	}
	if err := page.ExecuteTemplate(w, "layout", data); err != nil {
		// The response is already partly written by this point, so all that
		// is left is to record it.
		s.logf("rendering %s: %v", name, err)
	}
}

func (s *Server) logf(format string, args ...any) {
	if s.logger != nil {
		s.logger.Printf(format, args...)
	}
}

// errInvalidQuery is returned when a search request has nothing to search for.
var errInvalidQuery = errors.New("a search term is required")

func searchTerm(r *http.Request) (string, error) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		return "", errInvalidQuery
	}
	if len(q) > 100 {
		q = q[:100]
	}
	return q, nil
}
