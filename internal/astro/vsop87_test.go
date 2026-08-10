package astro

import (
	"bufio"
	"embed"
	"math"
	"strconv"
	"strings"
	"testing"
)

// vsop87.chk ships with the VSOP87 distribution and lists reference values for
// every solution at a spread of epochs. Checking against it proves both the
// file parser and the series evaluator.
//
//go:embed data/vsop87.chk
var checkFS embed.FS

type checkCase struct {
	body    string
	jde     float64
	l, b, r float64 // radians, radians, AU
}

func loadCheckCases(t *testing.T) []checkCase {
	t.Helper()
	data, err := checkFS.ReadFile("data/vsop87.chk")
	if err != nil {
		t.Fatalf("reading check file: %v", err)
	}

	var cases []checkCase
	var pending *checkCase
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		fields := strings.Fields(line)

		if len(fields) >= 3 && fields[0] == "VSOP87D" {
			jde, err := strconv.ParseFloat(strings.TrimPrefix(fields[2], "JD"), 64)
			if err != nil {
				t.Fatalf("parsing epoch %q: %v", fields[2], err)
			}
			pending = &checkCase{body: fields[1], jde: jde}
			continue
		}
		if pending == nil {
			continue
		}
		// The values line reads: l <rad> rad b <rad> rad r <au> au.
		// The line after it holds the derivatives (l', b', r') — skip those.
		if len(fields) == 9 && fields[0] == "l" && fields[3] == "b" && fields[6] == "r" {
			pending.l = mustFloat(t, fields[1])
			pending.b = mustFloat(t, fields[4])
			pending.r = mustFloat(t, fields[7])
			cases = append(cases, *pending)
			pending = nil
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scanning check file: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("no VSOP87D cases found in check file")
	}
	return cases
}

// mustFloat parses the check file's Fortran-style floats, which drop the
// leading zero (".0479095093", "-.0053006055").
func mustFloat(t *testing.T, s string) float64 {
	t.Helper()
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		t.Fatalf("parsing %q: %v", s, err)
	}
	return v
}

var checkBodies = map[string]vsopPlanet{
	"MERCURY": vMercury,
	"VENUS":   vVenus,
	"EARTH":   vEarth,
	"MARS":    vMars,
	"JUPITER": vJupiter,
	"SATURN":  vSaturn,
	"URANUS":  vUranus,
	"NEPTUNE": vNeptune,
}

func TestVSOP87DAgainstCheckFile(t *testing.T) {
	// The check file quotes ten decimal places of radians, so agreement to
	// 1e-10 rad (2e-5 arcsec) is the most the data can demonstrate. Allow a
	// little slack for the final rounded digit.
	const tolRad = 5e-10
	const tolAU = 5e-10

	cases := loadCheckCases(t)
	tested := 0
	for _, c := range cases {
		p, ok := checkBodies[c.body]
		if !ok {
			continue // EMB, SUN and the other solutions' bodies
		}
		l, b, r, err := heliocentric(p, c.jde)
		if err != nil {
			t.Fatalf("%s: %v", c.body, err)
		}
		// The check file writes longitude in [0, 2π); so does position.
		if d := math.Abs(arcRad(l, c.l)); d > tolRad {
			t.Errorf("%s JD%.1f: longitude %.10f, want %.10f (off by %.2e rad)", c.body, c.jde, l, c.l, d)
		}
		if d := math.Abs(b - c.b); d > tolRad {
			t.Errorf("%s JD%.1f: latitude %.10f, want %.10f (off by %.2e rad)", c.body, c.jde, b, c.b, d)
		}
		if d := math.Abs(r - c.r); d > tolAU {
			t.Errorf("%s JD%.1f: radius %.10f, want %.10f (off by %.2e AU)", c.body, c.jde, r, c.r, d)
		}
		tested++
	}
	if tested < 80 {
		t.Fatalf("only %d cases exercised; expected at least 80", tested)
	}
	t.Logf("validated %d VSOP87D reference positions", tested)
}

// arcRad returns the shortest signed difference between two angles in radians.
func arcRad(a, b float64) float64 {
	d := math.Mod(b-a, 2*math.Pi)
	if d > math.Pi {
		d -= 2 * math.Pi
	} else if d < -math.Pi {
		d += 2 * math.Pi
	}
	return d
}
