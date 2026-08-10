// Command gen rebuilds the embedded place and time-zone data.
//
// It downloads the GeoNames city dump, trims it to the handful of columns a
// birth chart needs, and writes a gzipped tab-separated file that
// internal/places embeds. It also writes the list of IANA time-zone names,
// taken from the Go toolchain's own copy of the zone database so that the
// dropdown can only ever offer zones time.LoadLocation will accept.
//
// Run it from the repository root:
//
//	go run ./internal/places/gen
//
// The generated files are committed, so an ordinary build needs no network.
package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	// cities5000 covers every populated place over 5,000 people — about
	// 70,000 entries, which is a reasonable trade between coverage of small
	// birth towns and the size of the binary.
	citiesURL   = "https://download.geonames.org/export/dump/cities5000.zip"
	countryURL  = "https://download.geonames.org/export/dump/countryInfo.txt"
	admin1URL   = "https://download.geonames.org/export/dump/admin1CodesASCII.txt"
	outCities   = "internal/places/data/cities.tsv.gz"
	outZones    = "internal/places/zones_gen.go"
	minimumRows = 50000
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("gen: ")

	countries, err := fetchCountries()
	if err != nil {
		log.Fatalf("country names: %v", err)
	}
	admin1, err := fetchAdmin1()
	if err != nil {
		log.Fatalf("admin1 names: %v", err)
	}
	rows, err := fetchCities(countries, admin1)
	if err != nil {
		log.Fatalf("cities: %v", err)
	}
	if len(rows) < minimumRows {
		log.Fatalf("only %d cities parsed, expected at least %d — the upstream format may have changed",
			len(rows), minimumRows)
	}
	if err := writeCities(rows); err != nil {
		log.Fatalf("writing %s: %v", outCities, err)
	}
	log.Printf("wrote %s (%d places)", outCities, len(rows))

	zones, err := readZoneNames()
	if err != nil {
		log.Fatalf("zone names: %v", err)
	}
	if err := writeZones(zones); err != nil {
		log.Fatalf("writing %s: %v", outZones, err)
	}
	log.Printf("wrote %s (%d zones)", outZones, len(zones))
}

func get(url string) ([]byte, error) {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// fetchCountries maps ISO country codes to country names.
func fetchCountries() (map[string]string, error) {
	data, err := get(countryURL)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) > 4 && f[0] != "" {
			out[f[0]] = f[4]
		}
	}
	return out, sc.Err()
}

// fetchAdmin1 maps "<country>.<admin1 code>" to the region's name.
func fetchAdmin1() (map[string]string, error) {
	data, err := get(admin1URL)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		f := strings.Split(sc.Text(), "\t")
		if len(f) > 1 {
			out[f[0]] = f[1]
		}
	}
	return out, sc.Err()
}

// Column indexes in the GeoNames city dump.
const (
	colName     = 1
	colASCII    = 2
	colLat      = 4
	colLon      = 5
	colCountry  = 8
	colAdmin1   = 10
	colPop      = 14
	colTimezone = 17
	colCount    = 19
)

func fetchCities(countries, admin1 map[string]string) ([]string, error) {
	data, err := get(citiesURL)
	if err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}

	var rows []string
	for _, f := range zr.File {
		if !strings.HasSuffix(f.Name, ".txt") {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		sc := bufio.NewScanner(rc)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			row, ok := cityRow(strings.Split(sc.Text(), "\t"), countries, admin1)
			if ok {
				rows = append(rows, row)
			}
		}
		err = sc.Err()
		rc.Close()
		if err != nil {
			return nil, err
		}
	}

	// Most populous first, so a prefix search naturally surfaces the place
	// the user most likely means.
	sort.SliceStable(rows, func(i, j int) bool {
		return populationOf(rows[i]) > populationOf(rows[j])
	})
	return rows, nil
}

func cityRow(f []string, countries, admin1 map[string]string) (string, bool) {
	if len(f) < colCount {
		return "", false
	}
	name, tz, cc := f[colName], f[colTimezone], f[colCountry]
	if name == "" || tz == "" {
		return "", false
	}

	lat, err := strconv.ParseFloat(f[colLat], 64)
	if err != nil {
		return "", false
	}
	lon, err := strconv.ParseFloat(f[colLon], 64)
	if err != nil {
		return "", false
	}
	pop, _ := strconv.Atoi(f[colPop])

	region := admin1[cc+"."+f[colAdmin1]]
	country := countries[cc]
	if country == "" {
		country = cc
	}

	// The ASCII spelling is only worth storing when it differs, which lets
	// searches for "Zurich" find "Zürich" without doubling the file.
	ascii := f[colASCII]
	if ascii == name {
		ascii = ""
	}

	// Four decimal places is about eleven metres — far finer than a birth
	// place is ever known to.
	return strings.Join([]string{
		name, ascii, region, country, cc,
		strconv.FormatFloat(lat, 'f', 4, 64),
		strconv.FormatFloat(lon, 'f', 4, 64),
		tz,
		strconv.Itoa(pop),
	}, "\t"), true
}

func populationOf(row string) int {
	i := strings.LastIndexByte(row, '\t')
	n, _ := strconv.Atoi(row[i+1:])
	return n
}

func writeCities(rows []string) error {
	if err := os.MkdirAll(filepath.Dir(outCities), 0o755); err != nil {
		return err
	}
	f, err := os.Create(outCities)
	if err != nil {
		return err
	}
	defer f.Close()

	zw, err := gzip.NewWriterLevel(f, gzip.BestCompression)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if _, err := io.WriteString(zw, row+"\n"); err != nil {
			return err
		}
	}
	if err := zw.Close(); err != nil {
		return err
	}
	return f.Close()
}

// readZoneNames lists the zones in the Go toolchain's zoneinfo.zip. Taking the
// list from there rather than from an external source guarantees every name
// offered in the dropdown is one time.LoadLocation can resolve, since the same
// database is what time/tzdata embeds.
func readZoneNames() ([]string, error) {
	path := filepath.Join(runtimeGOROOT(), "lib", "time", "zoneinfo.zip")
	zr, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	defer zr.Close()

	var zones []string
	for _, f := range zr.File {
		name := f.Name
		if f.FileInfo().IsDir() || !strings.Contains(name, "/") {
			continue // the top-level files are aliases like "UTC" and "Factory"
		}
		if strings.HasPrefix(name, "SystemV/") || strings.HasPrefix(name, "Etc/") {
			continue // legacy and fixed-offset zones, not real places
		}
		zones = append(zones, name)
	}
	zones = append(zones, "UTC")
	sort.Strings(zones)
	if len(zones) < 300 {
		return nil, fmt.Errorf("only %d zones found in %s", len(zones), path)
	}
	return zones, nil
}

func runtimeGOROOT() string {
	if v := os.Getenv("GOROOT"); v != "" {
		return v
	}
	out, err := runCommand("go", "env", "GOROOT")
	if err != nil {
		log.Fatalf("finding GOROOT: %v", err)
	}
	return strings.TrimSpace(out)
}

func writeZones(zones []string) error {
	var b strings.Builder
	b.WriteString("// Code generated by internal/places/gen. DO NOT EDIT.\n\n")
	b.WriteString("package places\n\n")
	b.WriteString("// zoneNames lists every IANA time zone in the database that time/tzdata\n")
	b.WriteString("// embeds, so each one is guaranteed to load.\n")
	b.WriteString("var zoneNames = []string{\n")
	for _, z := range zones {
		fmt.Fprintf(&b, "\t%q,\n", z)
	}
	b.WriteString("}\n")
	return os.WriteFile(outZones, []byte(b.String()), 0o644)
}
