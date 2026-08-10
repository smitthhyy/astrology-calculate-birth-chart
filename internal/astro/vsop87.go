package astro

import (
	"bufio"
	"bytes"
	"embed"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
)

// The VSOP87D solution (Bretagnon & Francou, 1988) gives heliocentric
// spherical coordinates referred to the mean dynamical ecliptic and equinox
// *of the date*, which is exactly the frame a natal chart is drawn in, so no
// precession step is needed on top of it.
//
//go:embed data/vsop87/VSOP87D.*
var vsop87FS embed.FS

// vsopPlanet identifies a body that has a VSOP87D coefficient table. It is an
// internal detail; callers work with Body instead.
type vsopPlanet int

const (
	vMercury vsopPlanet = iota
	vVenus
	vEarth
	vMars
	vJupiter
	vSaturn
	vUranus
	vNeptune
	numPlanets
)

// File extension and the body name as it is spelled in the file headers,
// indexed by vsopPlanet.
var (
	planetExt = [numPlanets]string{
		"mer", "ven", "ear", "mar", "jup", "sat", "ura", "nep",
	}
	planetLabel = [numPlanets]string{
		"MERCURY", "VENUS  ", "EARTH  ", "MARS   ",
		"JUPITER", "SATURN ", "URANUS ", "NEPTUNE",
	}
)

// term is one periodic term A·cos(B + C·τ) of a VSOP87 series.
type term struct{ a, b, c float64 }

// series holds one parsed VSOP87D file: three variables (L, B, R), each
// expanded in powers of τ from τ⁰ to τ⁵.
type series [3][6][]term

// sum evaluates variable v (0=L, 1=B, 2=R) at τ millennia from J2000.
func (s *series) sum(v int, τ float64) float64 {
	var total float64
	for p := 5; p >= 0; p-- {
		var acc float64
		for _, t := range s[v][p] {
			acc += t.a * math.Cos(t.b+t.c*τ)
		}
		total = total*τ + acc
	}
	return total
}

// position returns heliocentric longitude and latitude in radians and the
// radius vector in AU, for the mean ecliptic and equinox of date.
func (s *series) position(jde float64) (l, b, r float64) {
	τ := (jde - j2000) / 365250
	return pmod(s.sum(0, τ), 2*math.Pi), s.sum(1, τ), s.sum(2, τ)
}

// loadPlanet returns the coefficient table for p, parsing it on first use.
var loadPlanet = func() func(vsopPlanet) (*series, error) {
	loaders := make([]func() (*series, error), numPlanets)
	for i := range loaders {
		p := vsopPlanet(i)
		loaders[i] = sync.OnceValues(func() (*series, error) { return parsePlanet(p) })
	}
	return func(p vsopPlanet) (*series, error) {
		if p < 0 || p >= numPlanets {
			return nil, fmt.Errorf("astro: no VSOP87D table for planet %d", p)
		}
		return loaders[p]()
	}
}()

// heliocentric returns p's heliocentric ecliptic longitude and latitude in
// radians and its radius vector in AU, referred to the equinox of date.
func heliocentric(p vsopPlanet, jde float64) (l, b, r float64, err error) {
	s, err := loadPlanet(p)
	if err != nil {
		return 0, 0, 0, err
	}
	l, b, r = s.position(jde)
	return l, b, r, nil
}

// Fixed column positions in the VSOP87 file format. Header lines carry the
// variable, the power of τ and the term count; term lines carry A, B and C in
// their last three fields.
const (
	colVersion  = 16  // 'D' for the VSOP87D solution
	colBody     = 22  // .. 29, padded body name
	colVariable = 41  // '1', '2' or '3' for L, B, R
	colPower    = 59  // '0' .. '5'
	colCount    = 60  // .. 67, number of term lines that follow
	colA        = 79  // .. 97
	colB        = 98  // .. 111
	colC        = 111 // .. 131
	minTermLine = 131
)

func parsePlanet(p vsopPlanet) (*series, error) {
	name := "data/vsop87/VSOP87D." + planetExt[p]
	data, err := vsop87FS.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("astro: reading %s: %w", name, err)
	}

	s := new(series)
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 4096), 4096)

	var (
		lineNo    int
		v, pw     int
		remaining int
	)
	for sc.Scan() {
		lineNo++
		line := strings.TrimRight(sc.Text(), "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}

		if remaining == 0 {
			if v, pw, remaining, err = parseHeader(p, line); err != nil {
				return nil, fmt.Errorf("astro: %s line %d: %w", name, lineNo, err)
			}
			s[v][pw] = make([]term, 0, remaining)
			continue
		}

		t, err := parseTerm(line)
		if err != nil {
			return nil, fmt.Errorf("astro: %s line %d: %w", name, lineNo, err)
		}
		s[v][pw] = append(s[v][pw], t)
		remaining--
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("astro: scanning %s: %w", name, err)
	}
	if remaining != 0 {
		return nil, fmt.Errorf("astro: %s: truncated, %d terms missing", name, remaining)
	}
	return s, nil
}

// parseHeader reads a series header, returning the zero-based variable index,
// the power of τ and how many term lines follow.
func parseHeader(p vsopPlanet, line string) (v, pw, count int, err error) {
	if len(line) < colCount+7 {
		return 0, 0, 0, fmt.Errorf("short header line")
	}
	if line[colVersion] != 'D' {
		return 0, 0, 0, fmt.Errorf("expected a VSOP87D file, found version %q", line[colVersion])
	}
	if body := line[colBody : colBody+7]; body != planetLabel[p] {
		return 0, 0, 0, fmt.Errorf("expected body %q, found %q", planetLabel[p], body)
	}
	v = int(line[colVariable] - '1')
	if v < 0 || v > 2 {
		return 0, 0, 0, fmt.Errorf("bad variable %q", line[colVariable])
	}
	pw = int(line[colPower] - '0')
	if pw < 0 || pw > 5 {
		return 0, 0, 0, fmt.Errorf("bad power of tau %q", line[colPower])
	}
	count, err = strconv.Atoi(strings.TrimSpace(line[colCount : colCount+7]))
	if err != nil {
		return 0, 0, 0, fmt.Errorf("bad term count: %w", err)
	}
	if count < 0 {
		return 0, 0, 0, fmt.Errorf("negative term count %d", count)
	}
	return v, pw, count, nil
}

func parseTerm(line string) (term, error) {
	if len(line) < minTermLine {
		return term{}, fmt.Errorf("short term line (%d chars)", len(line))
	}
	a, err := parseField(line[colA:colB])
	if err != nil {
		return term{}, fmt.Errorf("field A: %w", err)
	}
	b, err := parseField(line[colB:colC])
	if err != nil {
		return term{}, fmt.Errorf("field B: %w", err)
	}
	c, err := parseField(line[colC:minTermLine])
	if err != nil {
		return term{}, fmt.Errorf("field C: %w", err)
	}
	return term{a, b, c}, nil
}

func parseField(s string) (float64, error) {
	return strconv.ParseFloat(strings.TrimSpace(s), 64)
}
