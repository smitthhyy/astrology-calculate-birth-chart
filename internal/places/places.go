// Package places resolves a birth place typed as free text into the
// coordinates and time zone a chart needs.
//
// The lookup is offline: a trimmed extract of the GeoNames gazetteer is
// compiled into the binary, so nothing about the birth leaves the machine.
// Package-level Geocode covers the rare place the extract does not have, but
// only when the caller asks for it.
package places

import (
	"bufio"
	"compress/gzip"
	"embed"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	_ "time/tzdata" // so every zone in zoneNames resolves, whatever the host has
)

//go:embed data/cities.tsv.gz
var dataFS embed.FS

// Place is a populated place that can be used as a birth place.
type Place struct {
	Name        string  `json:"name"`
	Region      string  `json:"region,omitempty"`
	Country     string  `json:"country"`
	CountryCode string  `json:"countryCode,omitempty"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	Timezone    string  `json:"timezone"`
	Population  int     `json:"population,omitempty"`
	// Label is the single line shown in the picker.
	Label string `json:"label"`

	// searchName is the accent-folded, lower-case name; searchASCII is the
	// gazetteer's own transliteration, kept because it sometimes differs
	// usefully (GeoNames renders "Zürich" as "Zuerich").
	searchName  string
	searchASCII string
}

func (p Place) buildLabel() string {
	parts := []string{p.Name}
	if p.Region != "" && p.Region != p.Name {
		parts = append(parts, p.Region)
	}
	if p.Country != "" {
		parts = append(parts, p.Country)
	}
	return strings.Join(parts, ", ")
}

// Columns in the embedded extract, in the order internal/places/gen writes them.
const (
	fName = iota
	fASCII
	fRegion
	fCountry
	fCountryCode
	fLat
	fLon
	fTimezone
	fPopulation
	fieldCount
)

// all returns the gazetteer, decompressed and parsed on first use. It is
// sorted most-populous first, which the search relies on for tie-breaking.
var all = sync.OnceValues(func() ([]Place, error) {
	f, err := dataFS.Open("data/cities.tsv.gz")
	if err != nil {
		return nil, fmt.Errorf("places: opening gazetteer: %w", err)
	}
	defer f.Close()

	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("places: decompressing gazetteer: %w", err)
	}
	defer zr.Close()

	var out []Place
	sc := bufio.NewScanner(zr)
	sc.Buffer(make([]byte, 0, 4096), 64*1024)
	for sc.Scan() {
		fields := strings.Split(sc.Text(), "\t")
		if len(fields) != fieldCount {
			continue
		}
		lat, err1 := strconv.ParseFloat(fields[fLat], 64)
		lon, err2 := strconv.ParseFloat(fields[fLon], 64)
		if err1 != nil || err2 != nil {
			continue
		}
		pop, _ := strconv.Atoi(fields[fPopulation])

		p := Place{
			Name:        fields[fName],
			Region:      fields[fRegion],
			Country:     fields[fCountry],
			CountryCode: fields[fCountryCode],
			Latitude:    lat,
			Longitude:   lon,
			Timezone:    fields[fTimezone],
			Population:  pop,
			searchName:  fold(fields[fName]),
			searchASCII: fold(fields[fASCII]),
		}
		p.Label = p.buildLabel()
		out = append(out, p)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("places: reading gazetteer: %w", err)
	}
	return out, nil
})

// Count returns how many places are in the gazetteer.
func Count() (int, error) {
	ps, err := all()
	return len(ps), err
}

// Search returns up to limit places matching the query, best first.
//
// The query may name the place alone ("ballarat") or narrow it with commas
// ("springfield, illinois", "san felipe, zambales, philippines"); everything
// after the first comma is matched against the region and country.
func Search(query string, limit int) ([]Place, error) {
	ps, err := all()
	if err != nil {
		return nil, err
	}

	name, where := splitQuery(query)
	if name == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}

	type scored struct {
		place Place
		score int
	}
	var hits []scored
	for _, p := range ps {
		score := matchName(p, name)
		if score == 0 {
			continue
		}
		if len(where) > 0 {
			bonus, ok := matchWhere(p, where)
			if !ok {
				continue
			}
			score += bonus
		}
		hits = append(hits, scored{p, score})
	}

	// The gazetteer is already ordered by population, so a stable sort on
	// score alone leaves the largest place first within each score band.
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].score > hits[j].score })

	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]Place, len(hits))
	for i, h := range hits {
		out[i] = h.place
	}
	return out, nil
}

// splitQuery separates "place, where, where…" into the place name and the
// qualifiers after it, all folded.
//
// The qualifiers are kept apart rather than treated as one string, because
// people write an address the way the post office wants it and give the
// province as well as the country: "San Felipe, Zambales, Philippines". Matched
// as a single lump that finds nothing, and the place appears not to exist.
func splitQuery(query string) (name string, where []string) {
	parts := strings.Split(fold(strings.TrimSpace(query)), ",")
	name = strings.TrimSpace(parts[0])
	for _, part := range parts[1:] {
		if part = strings.TrimSpace(part); part != "" {
			where = append(where, part)
		}
	}
	return name, where
}

// matchName scores a place against the place-name part of a query: an exact
// match beats a prefix, which beats a match anywhere in the name.
func matchName(p Place, name string) int {
	for _, candidate := range [2]string{p.searchName, p.searchASCII} {
		if candidate == "" {
			continue
		}
		switch {
		case candidate == name:
			return 100
		case strings.HasPrefix(candidate, name):
			return 50
		case strings.Contains(candidate, name):
			return 10
		}
	}
	return 0
}

// matchWhere scores a place against the qualifiers after the place name, and
// reports whether any of them matched at all.
//
// A qualifier that matches nothing is ignored rather than fatal, so long as
// another one matched. The gazetteer stores a country and a top-level region
// and nothing below that, so the province in "San Felipe, Zambales,
// Philippines" corresponds to no field and never will; insisting that every
// part match would reject the query for naming a real place too precisely.
// Scoring each part separately also means the ones that do match still rank
// the result, so a place agreeing with two qualifiers beats one agreeing with
// one.
func matchWhere(p Place, where []string) (score int, matched bool) {
	for _, part := range where {
		best := 0
		for _, candidate := range [3]string{
			fold(p.Country),
			fold(p.Region),
			fold(p.CountryCode),
		} {
			if candidate == "" {
				continue
			}
			switch {
			case candidate == part:
				best = max(best, 40)
			case strings.HasPrefix(candidate, part):
				best = max(best, 20)
			case strings.Contains(candidate, part):
				best = max(best, 5)
			}
		}
		score += best
		matched = matched || best > 0
	}
	return score, matched
}

// Resolve finds the single best place for a name and country, for form
// submissions where the user typed rather than picked. Country may be empty.
func Resolve(name, country string) (Place, error) {
	query := name
	if country != "" {
		query += ", " + country
	}
	matches, err := Search(query, 1)
	if err != nil {
		return Place{}, err
	}
	if len(matches) == 0 {
		return Place{}, fmt.Errorf("no place matching %q", strings.TrimSpace(query))
	}
	return matches[0], nil
}

// ZonesNear returns the distinct time zones of the n gazetteer places closest
// to a point, nearest first, along with the label of the closest one.
//
// It exists to sanity-check the zone a chart is about to be cast in. Returning
// a set rather than a single zone matters near a zone border, where the
// closest town can easily be on the other side of it: any of these is a
// defensible answer for the point, and only a zone outside the set is worth
// questioning.
func ZonesNear(lat, lon float64, n int) (zones []string, nearest string, err error) {
	ps, err := all()
	if err != nil {
		return nil, "", err
	}
	if n <= 0 {
		n = 12
	}

	// Rank by an equirectangular approximation. Ranking is all that is needed,
	// so the cost of a great-circle distance for seventy thousand rows is not.
	type candidate struct {
		place Place
		d2    float64
	}
	best := make([]candidate, 0, n+1)
	cosLat := math.Cos(lat * math.Pi / 180)

	for _, p := range ps {
		dLat := p.Latitude - lat
		dLon := shortestLonDelta(p.Longitude, lon) * cosLat
		d2 := dLat*dLat + dLon*dLon

		if len(best) == n && d2 >= best[len(best)-1].d2 {
			continue
		}
		i := sort.Search(len(best), func(i int) bool { return best[i].d2 > d2 })
		best = append(best, candidate{})
		copy(best[i+1:], best[i:])
		best[i] = candidate{p, d2}
		if len(best) > n {
			best = best[:n]
		}
	}
	if len(best) == 0 {
		return nil, "", nil
	}

	seen := map[string]bool{}
	for _, c := range best {
		if c.place.Timezone != "" && !seen[c.place.Timezone] {
			seen[c.place.Timezone] = true
			zones = append(zones, c.place.Timezone)
		}
	}
	return zones, best[0].place.Label, nil
}

// shortestLonDelta returns the difference between two longitudes, taking the
// short way round the antimeridian.
func shortestLonDelta(a, b float64) float64 {
	d := math.Mod(a-b, 360)
	if d > 180 {
		d -= 360
	} else if d < -180 {
		d += 360
	}
	return d
}

// Zone is an IANA time zone offered in the picker.
type Zone struct {
	Name   string `json:"name"`
	Label  string `json:"label"`
	Offset int    `json:"offset"` // current offset from UTC, in seconds
}

// Zones returns every selectable time zone, ordered west to east and then by
// name, each labelled with the offset in force today. The offset is only a
// hint for picking the right zone; the chart itself uses the offset that was
// in force on the birth date.
var Zones = sync.OnceValue(func() []Zone {
	now := time.Now()
	zones := make([]Zone, 0, len(zoneNames))
	for _, name := range zoneNames {
		loc, err := time.LoadLocation(name)
		if err != nil {
			continue
		}
		_, offset := now.In(loc).Zone()
		zones = append(zones, Zone{
			Name:   name,
			Label:  fmt.Sprintf("(UTC%s) %s", formatOffset(offset), name),
			Offset: offset,
		})
	}
	sort.SliceStable(zones, func(i, j int) bool {
		if zones[i].Offset != zones[j].Offset {
			return zones[i].Offset < zones[j].Offset
		}
		return zones[i].Name < zones[j].Name
	})
	return zones
})

// LookupZone loads a time zone by IANA name, rejecting anything not on the
// offered list so a request cannot reach for an arbitrary string.
func LookupZone(name string) (*time.Location, error) {
	for _, z := range zoneNames {
		if z == name {
			return time.LoadLocation(name)
		}
	}
	return nil, fmt.Errorf("places: %q is not a known time zone", name)
}

func formatOffset(seconds int) string {
	sign := "+"
	if seconds < 0 {
		sign, seconds = "-", -seconds
	}
	return fmt.Sprintf("%s%02d:%02d", sign, seconds/3600, (seconds%3600)/60)
}
