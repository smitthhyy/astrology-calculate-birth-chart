package places

import (
	"strings"
	"testing"
	"time"
)

func TestGazetteerLoads(t *testing.T) {
	n, err := Count()
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if n < 50000 {
		t.Errorf("gazetteer holds %d places, expected at least 50000", n)
	}
}

func TestSearch(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		wantFirst string // expected Label of the top hit
		wantZone  string
	}{
		{
			name:  "plain name picks the largest match",
			query: "ballarat", wantFirst: "Ballarat, Victoria, Australia",
			wantZone: "Australia/Melbourne",
		},
		{
			name:  "country narrows an ambiguous name",
			query: "springfield, united states", wantZone: "America/Chicago",
		},
		{
			name:  "region narrows an ambiguous name",
			query: "springfield, illinois", wantFirst: "Springfield, Illinois, United States",
		},
		{
			name:  "ascii spelling finds an accented name",
			query: "zurich", wantFirst: "Zürich, Zurich, Switzerland",
			wantZone: "Europe/Zurich",
		},
		{
			name:  "case and padding are ignored",
			query: "  HONOLULU ", wantFirst: "Honolulu, Hawaii, United States",
			wantZone: "Pacific/Honolulu",
		},
		{
			name:  "country code works as the qualifier",
			query: "london, gb", wantFirst: "London, England, United Kingdom",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Search(tc.query, 5)
			if err != nil {
				t.Fatalf("Search: %v", err)
			}
			if len(got) == 0 {
				t.Fatalf("no results for %q", tc.query)
			}
			if tc.wantFirst != "" && got[0].Label != tc.wantFirst {
				t.Errorf("top hit %q, want %q", got[0].Label, tc.wantFirst)
			}
			if tc.wantZone != "" && got[0].Timezone != tc.wantZone {
				t.Errorf("time zone %q, want %q", got[0].Timezone, tc.wantZone)
			}
		})
	}
}

// People write an address the way it is written on an envelope, with the
// province in the middle. The gazetteer stores a country and a top-level
// region and nothing below, so a province matches no field at all — and while
// everything after the first comma was treated as one string, naming it made
// the place vanish. "San Felipe, Zambales, Philippines" is a real town that
// reported this.
func TestSearchToleratesAQualifierItHasNoFieldFor(t *testing.T) {
	tests := []struct {
		name        string
		query       string
		wantFirst   string // exact label, where the qualifiers settle it
		wantCountry string // where an unknown qualifier leaves the choice open
	}{
		{
			name:  "province between the town and the country",
			query: "san felipe, zambales, philippines",
			// The gazetteer's own name for the place; the point is the country.
			wantFirst: "Poblacion, San Felipe, Central Luzon, Philippines",
		},
		{
			// The county cannot rank anything, so the largest Springfield in the
			// country wins — Missouri, not the Illinois one the county names. The
			// unknown qualifier is ignored, not honoured, and the guarantee is
			// only that the country was.
			name:        "county between the town and the country",
			query:       "springfield, sangamon, united states",
			wantCountry: "United States",
		},
		{
			name:      "region and country together, both known",
			query:     "springfield, illinois, united states",
			wantFirst: "Springfield, Illinois, United States",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Search(tc.query, 5)
			if err != nil {
				t.Fatalf("Search: %v", err)
			}
			if len(got) == 0 {
				t.Fatalf("no results for %q", tc.query)
			}
			if tc.wantFirst != "" && got[0].Label != tc.wantFirst {
				t.Errorf("top hit %q, want %q", got[0].Label, tc.wantFirst)
			}
			if tc.wantCountry != "" && got[0].Country != tc.wantCountry {
				t.Errorf("top hit %q is not in %s", got[0].Label, tc.wantCountry)
			}
		})
	}
}

// Tolerating one unrecognised qualifier must not amount to ignoring all of
// them: a qualifier is still how an ambiguous name is narrowed, and if none of
// them matches, the place named is not the place meant.
func TestSearchStillRejectsAWhollyWrongQualifier(t *testing.T) {
	got, err := Search("san felipe, zzz nowhere", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d results, want none: %s", len(got), got[0].Label)
	}
}

// Two qualifiers that both match beat one that matches, so naming the region
// as well as the country sharpens the result rather than merely permitting it.
func TestSearchPrefersThePlaceMatchingMoreQualifiers(t *testing.T) {
	got, err := Search("springfield, illinois, united states", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) == 0 || got[0].Region != "Illinois" {
		t.Fatalf("top hit is not in Illinois: %+v", got)
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	got, err := Search("   ", 5)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d results for an empty query, want none", len(got))
	}
}

func TestSearchRespectsLimit(t *testing.T) {
	got, err := Search("san", 7)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 7 {
		t.Errorf("got %d results, want 7", len(got))
	}
}

func TestResolve(t *testing.T) {
	p, err := Resolve("Ulm", "Germany")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if p.Timezone != "Europe/Berlin" {
		t.Errorf("time zone %q, want Europe/Berlin", p.Timezone)
	}
	if p.Latitude < 48.3 || p.Latitude > 48.5 {
		t.Errorf("latitude %.4f is not near Ulm", p.Latitude)
	}

	if _, err := Resolve("Nowhere At All Zzz", ""); err == nil {
		t.Error("expected an error for an unknown place")
	}
}

func TestEveryPlaceHasALoadableZone(t *testing.T) {
	ps, err := all()
	if err != nil {
		t.Fatalf("loading gazetteer: %v", err)
	}
	// Zone names repeat heavily across places, so check each distinct one once.
	checked := map[string]bool{}
	for _, p := range ps {
		if checked[p.Timezone] {
			continue
		}
		checked[p.Timezone] = true
		if _, err := time.LoadLocation(p.Timezone); err != nil {
			t.Errorf("place %q has unloadable zone %q: %v", p.Label, p.Timezone, err)
		}
	}
	t.Logf("checked %d distinct time zones", len(checked))
}

func TestZones(t *testing.T) {
	zones := Zones()
	if len(zones) < 300 {
		t.Fatalf("got %d zones, want at least 300", len(zones))
	}
	// Ordered west to east.
	for i := 1; i < len(zones); i++ {
		if zones[i].Offset < zones[i-1].Offset {
			t.Fatalf("zones are not ordered by offset at index %d", i)
		}
	}
	var found bool
	for _, z := range zones {
		if z.Name == "Australia/Melbourne" {
			found = true
			if !strings.HasPrefix(z.Label, "(UTC+1") {
				t.Errorf("Melbourne label %q does not show a +10/+11 offset", z.Label)
			}
		}
	}
	if !found {
		t.Error("Australia/Melbourne is missing from the zone list")
	}
}

func TestLookupZone(t *testing.T) {
	if _, err := LookupZone("Europe/London"); err != nil {
		t.Errorf("LookupZone(Europe/London): %v", err)
	}
	if _, err := LookupZone("Mars/Olympus_Mons"); err == nil {
		t.Error("expected an error for an unknown zone")
	}
	if _, err := LookupZone("../../etc/passwd"); err == nil {
		t.Error("expected an error for a path-like zone name")
	}
}

func TestFold(t *testing.T) {
	tests := map[string]string{
		"Zürich":    "zurich",
		"São Paulo": "sao paulo",
		"München":   "munchen",
		"Kraków":    "krakow",
		"Ōsaka":     "osaka",
		"Ballarat":  "ballarat",
	}
	for in, want := range tests {
		if got := fold(in); got != want {
			t.Errorf("fold(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSearchFindsAccentedNamesUnaccented(t *testing.T) {
	// GeoNames uses the English exonym where one is well established, so
	// Munich is "Munich" rather than "München"; these are the cases where the
	// gazetteer's own spelling carries an accent.
	tests := map[string]string{
		"zurich":    "Europe/Zurich",
		"sao paulo": "America/Sao_Paulo",
		"koln":      "Europe/Berlin",
		"malmo":     "Europe/Stockholm",
		"krakow":    "Europe/Warsaw",
	}
	for query, wantZone := range tests {
		got, err := Search(query, 1)
		if err != nil {
			t.Fatalf("Search(%q): %v", query, err)
		}
		if len(got) == 0 {
			t.Errorf("no results for %q", query)
			continue
		}
		if got[0].Timezone != wantZone {
			t.Errorf("%q -> %s (%s), want zone %s", query, got[0].Label, got[0].Timezone, wantZone)
		}
	}
}
