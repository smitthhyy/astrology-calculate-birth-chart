package places

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// geocodeEndpoint is Open-Meteo's geocoding service: free, keyless, and backed
// by the full GeoNames gazetteer rather than the extract embedded here.
const geocodeEndpoint = "https://geocoding-api.open-meteo.com/v1/search"

// Geocode looks a place up over the network.
//
// This is the only part of the application that talks to the internet, and it
// runs only when the user explicitly asks for it, because it necessarily sends
// the birth place to a third party. Offline Search covers roughly seventy
// thousand places and should be tried first.
func Geocode(ctx context.Context, query string, limit int) ([]Place, error) {
	if limit <= 0 {
		limit = 10
	}
	q := url.Values{
		"name":     {query},
		"count":    {strconv.Itoa(limit)},
		"language": {"en"},
		"format":   {"json"},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, geocodeEndpoint+"?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("places: online lookup failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("places: online lookup returned %s", resp.Status)
	}

	var body struct {
		Results []struct {
			Name        string  `json:"name"`
			Latitude    float64 `json:"latitude"`
			Longitude   float64 `json:"longitude"`
			Timezone    string  `json:"timezone"`
			Country     string  `json:"country"`
			CountryCode string  `json:"country_code"`
			Admin1      string  `json:"admin1"`
			Population  int     `json:"population"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("places: unreadable response from the geocoding service: %w", err)
	}

	out := make([]Place, 0, len(body.Results))
	for _, r := range body.Results {
		if r.Timezone == "" {
			continue // without a zone the result cannot be used for a chart
		}
		p := Place{
			Name:        r.Name,
			Region:      r.Admin1,
			Country:     r.Country,
			CountryCode: r.CountryCode,
			Latitude:    r.Latitude,
			Longitude:   r.Longitude,
			Timezone:    r.Timezone,
			Population:  r.Population,
		}
		p.Label = p.buildLabel()
		out = append(out, p)
	}
	return out, nil
}
