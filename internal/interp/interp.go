// Package interp holds the written meanings that turn a set of coordinates
// into a readable birth chart.
//
// The text lives in embedded JSON, one file per body, so it can be edited or
// translated without touching Go code. Every body carries one constant line
// saying what it governs, plus a reading for each of the twelve signs.
package interp

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"sync"
)

//go:embed data/bodies/*.json data/houses/*.json data/points/*.json data/aspects/*.json
var dataFS embed.FS

// Reading is what one body in one sign means.
type Reading struct {
	// Definition says what the placement is.
	Definition string `json:"definition"`
	// ShowsUp says how it is recognisable in someone's life.
	ShowsUp string `json:"showsUp"`
}

// Body is the full set of text for one chart point.
type Body struct {
	Key  string `json:"key"`
	Name string `json:"name"`
	// Defines is the "what it defines" column: constant for the body,
	// independent of the sign it happens to fall in.
	Defines string `json:"defines"`
	// Signs is keyed by the lower-case sign name, "aries" through "pisces".
	Signs map[string]Reading `json:"signs"`
}

var signKeys = [12]string{
	"aries", "taurus", "gemini", "cancer", "leo", "virgo",
	"libra", "scorpio", "sagittarius", "capricorn", "aquarius", "pisces",
}

// load reads and validates every body file once.
var load = sync.OnceValues(func() (map[string]Body, error) {
	entries, err := fs.Glob(dataFS, "data/bodies/*.json")
	if err != nil {
		return nil, err
	}

	bodies := make(map[string]Body, len(entries))
	for _, name := range entries {
		raw, err := dataFS.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("interp: reading %s: %w", name, err)
		}
		var b Body
		if err := json.Unmarshal(raw, &b); err != nil {
			return nil, fmt.Errorf("interp: parsing %s: %w", name, err)
		}
		if b.Key == "" {
			return nil, fmt.Errorf("interp: %s has no key", name)
		}
		if b.Defines == "" {
			return nil, fmt.Errorf("interp: %s has no defines line", name)
		}
		for _, sign := range signKeys {
			r, ok := b.Signs[sign]
			if !ok {
				return nil, fmt.Errorf("interp: %s is missing the %s reading", name, sign)
			}
			if r.Definition == "" || r.ShowsUp == "" {
				return nil, fmt.Errorf("interp: %s has an incomplete %s reading", name, sign)
			}
		}
		if _, dup := bodies[b.Key]; dup {
			return nil, fmt.Errorf("interp: %s repeats the key %q", name, b.Key)
		}
		bodies[b.Key] = b
	}
	return bodies, nil
})

// Lookup returns the reading for a body in a sign, both named by their
// lower-case keys. It reports false when the body has no written text, which
// is the case for the secondary points such as the Midheaven.
func Lookup(bodyKey, signKey string) (Body, Reading, bool) {
	bodies, err := load()
	if err != nil {
		return Body{}, Reading{}, false
	}
	b, ok := bodies[bodyKey]
	if !ok {
		return Body{}, Reading{}, false
	}
	r, ok := b.Signs[signKey]
	if !ok {
		return b, Reading{}, false
	}
	return b, r, true
}

// Defines returns just the "what it defines" line for a body.
func Defines(bodyKey string) (string, bool) {
	bodies, err := load()
	if err != nil {
		return "", false
	}
	b, ok := bodies[bodyKey]
	return b.Defines, ok
}

// houseText holds the readings for one house, keyed by the sign on its cusp.
// A house needs only one line per sign: what the house governs does not change
// with the chart and lives beside the geometry, so the sign-dependent part is
// all that varies here.
type houseText struct {
	Number int               `json:"number"`
	Signs  map[string]string `json:"signs"`
}

// loadHouses reads and validates the twelve house files once.
var loadHouses = sync.OnceValues(func() (map[int]houseText, error) {
	entries, err := fs.Glob(dataFS, "data/houses/*.json")
	if err != nil {
		return nil, err
	}

	houses := make(map[int]houseText, len(entries))
	for _, name := range entries {
		raw, err := dataFS.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("interp: reading %s: %w", name, err)
		}
		var h houseText
		if err := json.Unmarshal(raw, &h); err != nil {
			return nil, fmt.Errorf("interp: parsing %s: %w", name, err)
		}
		if h.Number < 1 || h.Number > 12 {
			return nil, fmt.Errorf("interp: %s gives house number %d", name, h.Number)
		}
		if _, dup := houses[h.Number]; dup {
			return nil, fmt.Errorf("interp: %s repeats house %d", name, h.Number)
		}
		for _, sign := range signKeys {
			if h.Signs[sign] == "" {
				return nil, fmt.Errorf("interp: %s is missing the %s reading", name, sign)
			}
		}
		houses[h.Number] = h
	}
	for n := 1; n <= 12; n++ {
		if _, ok := houses[n]; !ok {
			return nil, fmt.Errorf("interp: no text for house %d", n)
		}
	}
	return houses, nil
})

// HouseReading returns how a house tends to show up with a given sign on its
// cusp. Number runs from 1 to 12; signKey is the lower-case sign name.
func HouseReading(number int, signKey string) (string, bool) {
	houses, err := loadHouses()
	if err != nil {
		return "", false
	}
	h, ok := houses[number]
	if !ok {
		return "", false
	}
	text, ok := h.Signs[signKey]
	return text, ok
}

// pointText holds the readings for one secondary chart point — the Midheaven,
// the Imum Coeli and the two lunar nodes. These carry no separate definition:
// unlike a planet, there is nothing to say about the point beyond what it
// governs and how that sign inflects it, so one line per sign is the whole of
// it.
type pointText struct {
	Key     string            `json:"key"`
	Name    string            `json:"name"`
	Defines string            `json:"defines"`
	Signs   map[string]string `json:"signs"`
}

// loadPoints reads and validates the secondary-point files once.
var loadPoints = sync.OnceValues(func() (map[string]pointText, error) {
	entries, err := fs.Glob(dataFS, "data/points/*.json")
	if err != nil {
		return nil, err
	}

	points := make(map[string]pointText, len(entries))
	for _, name := range entries {
		raw, err := dataFS.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("interp: reading %s: %w", name, err)
		}
		var p pointText
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("interp: parsing %s: %w", name, err)
		}
		if p.Key == "" {
			return nil, fmt.Errorf("interp: %s has no key", name)
		}
		if p.Defines == "" {
			return nil, fmt.Errorf("interp: %s has no defines line", name)
		}
		for _, sign := range signKeys {
			if p.Signs[sign] == "" {
				return nil, fmt.Errorf("interp: %s is missing the %s reading", name, sign)
			}
		}
		if _, dup := points[p.Key]; dup {
			return nil, fmt.Errorf("interp: %s repeats the key %q", name, p.Key)
		}
		points[p.Key] = p
	}
	return points, nil
})

// PointDefines returns the "what it defines" line for a secondary point.
func PointDefines(key string) (string, bool) {
	points, err := loadPoints()
	if err != nil {
		return "", false
	}
	p, ok := points[key]
	return p.Defines, ok
}

// PointReading returns how a secondary point tends to show up in a given sign.
func PointReading(key, signKey string) (string, bool) {
	points, err := loadPoints()
	if err != nil {
		return "", false
	}
	p, ok := points[key]
	if !ok {
		return "", false
	}
	text, ok := p.Signs[signKey]
	return text, ok
}

// Points returns the keys of every secondary point that has written text.
func Points() []string {
	points, err := loadPoints()
	if err != nil {
		return nil
	}
	keys := make([]string, 0, len(points))
	for k := range points {
		keys = append(keys, k)
	}
	return keys
}

// The four tones an aspect is written in.
//
// There are five Ptolemaic aspects but only four readings, because the trine
// and the sextile differ in strength rather than in kind: both say the two
// bodies cooperate, the trine simply more so. Writing two versions of the same
// sentence would be padding, and a reader would never see both — a given pair
// of bodies makes at most one aspect in a chart. The square and the opposition
// are kept apart, because friction you feel inside yourself and friction that
// arrives through other people are genuinely different experiences.
const (
	modeConjunction = "conjunction"
	modeHarmonious  = "harmonious"
	modeSquare      = "square"
	modeOpposition  = "opposition"
)

var aspectModes = [4]string{modeConjunction, modeHarmonious, modeSquare, modeOpposition}

// aspectFile is one body's readings against every body that follows it in the
// canonical order, so each pair is written down exactly once.
type aspectFile struct {
	Body string                       `json:"body"`
	With map[string]map[string]string `json:"with"`
}

// pairKey identifies a pair of bodies regardless of which was named first.
func pairKey(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "\x00" + b
}

// loadAspects reads and validates the aspect files once, flattening them to a
// lookup keyed by body pair.
var loadAspects = sync.OnceValues(func() (map[string]map[string]string, error) {
	entries, err := fs.Glob(dataFS, "data/aspects/*.json")
	if err != nil {
		return nil, err
	}

	pairs := make(map[string]map[string]string)
	for _, name := range entries {
		raw, err := dataFS.ReadFile(name)
		if err != nil {
			return nil, fmt.Errorf("interp: reading %s: %w", name, err)
		}
		var f aspectFile
		if err := json.Unmarshal(raw, &f); err != nil {
			return nil, fmt.Errorf("interp: parsing %s: %w", name, err)
		}
		if f.Body == "" {
			return nil, fmt.Errorf("interp: %s does not name a body", name)
		}
		for other, readings := range f.With {
			for _, mode := range aspectModes {
				if readings[mode] == "" {
					return nil, fmt.Errorf("interp: %s has no %s reading for %s and %s",
						name, mode, f.Body, other)
				}
			}
			key := pairKey(f.Body, other)
			if _, dup := pairs[key]; dup {
				return nil, fmt.Errorf("interp: %s repeats the pair %s and %s", name, f.Body, other)
			}
			pairs[key] = readings
		}
	}
	return pairs, nil
})

// modeOf maps an aspect's name onto the tone its reading is written in.
func modeOf(aspectName string) (string, bool) {
	switch aspectName {
	case "Conjunction":
		return modeConjunction, true
	case "Trine", "Sextile":
		return modeHarmonious, true
	case "Square":
		return modeSquare, true
	case "Opposition":
		return modeOpposition, true
	}
	return "", false
}

// AspectReading returns how an aspect between two bodies tends to show up. The
// bodies are given by their lower-case keys in either order; aspectName is the
// aspect's display name, such as "Trine".
func AspectReading(bodyA, bodyB, aspectName string) (string, bool) {
	mode, ok := modeOf(aspectName)
	if !ok {
		return "", false
	}
	pairs, err := loadAspects()
	if err != nil {
		return "", false
	}
	readings, ok := pairs[pairKey(bodyA, bodyB)]
	if !ok {
		return "", false
	}
	text, ok := readings[mode]
	return text, ok
}

// Validate reports any problem with the embedded text. main calls it at
// startup so a malformed data file fails loudly rather than silently blanking
// a column.
func Validate() error {
	if _, err := load(); err != nil {
		return err
	}
	if _, err := loadHouses(); err != nil {
		return err
	}
	if _, err := loadPoints(); err != nil {
		return err
	}
	_, err := loadAspects()
	return err
}

// Bodies returns the keys of every body that has written text.
func Bodies() []string {
	bodies, err := load()
	if err != nil {
		return nil
	}
	keys := make([]string, 0, len(bodies))
	for k := range bodies {
		keys = append(keys, k)
	}
	return keys
}
