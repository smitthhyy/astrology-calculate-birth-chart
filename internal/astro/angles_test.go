package astro

import (
	"math"
	"testing"
)

func TestSplitDMS(t *testing.T) {
	cases := []struct {
		deg     float64
		d, m    int
		s       float64
		within  float64
		rounded string
	}{
		{deg: 0, d: 0, m: 0, s: 0, rounded: `0°00'00"`},
		{deg: 14.5, d: 14, m: 30, s: 0, rounded: `14°30'00"`},
		{deg: 29.999722, d: 29, m: 59, s: 59, within: 0.01, rounded: `29°59'58"`},
		{deg: 1.0 / 3600, d: 0, m: 0, s: 1, within: 1e-9, rounded: `0°00'01"`},
		// A hair under a whole minute must stay in the minute below, not round up.
		{deg: 4.999999, d: 4, m: 59, s: 59.9964, within: 0.01, rounded: `4°59'59"`},
	}

	for _, tc := range cases {
		d, m, s := SplitDMS(tc.deg)
		if d != tc.d || m != tc.m {
			t.Errorf("SplitDMS(%v) = %d°%d', want %d°%d'", tc.deg, d, m, tc.d, tc.m)
		}
		if math.Abs(s-tc.s) > tc.within {
			t.Errorf("SplitDMS(%v) seconds = %v, want %v", tc.deg, s, tc.s)
		}
		if got := FormatDMSSeconds(tc.deg); got != tc.rounded {
			t.Errorf("FormatDMSSeconds(%v) = %s, want %s", tc.deg, got, tc.rounded)
		}
	}
}

// The three parts must add back up to the angle they came from, which is what
// makes the truncation safe: a consumer reassembling them gets the same degree,
// never a fraction more.
func TestSplitDMSRoundTrips(t *testing.T) {
	for deg := 0.0; deg < 30; deg += 0.017 {
		d, m, s := SplitDMS(deg)
		back := float64(d) + float64(m)/60 + s/3600
		if math.Abs(back-deg) > 1e-12 {
			t.Fatalf("SplitDMS(%v) reassembles to %v", deg, back)
		}
	}
}

// Truncation is the point of the format: 29°59'59.9" is still in its own sign,
// and must not be shown as the 30° that begins the next one.
func TestFormattingNeverRollsIntoTheNextSign(t *testing.T) {
	const justUnder = 60 - 1e-6 // a whisker inside Taurus's last second

	if got, want := FormatPosition(justUnder), "29°59' Taurus"; got != want {
		t.Errorf("FormatPosition = %q, want %q", got, want)
	}
	if got, want := FormatPositionSeconds(justUnder), `29°59'59" Taurus`; got != want {
		t.Errorf("FormatPositionSeconds = %q, want %q", got, want)
	}
}

// The arcsecond form has to be the arcminute form with more of the same number
// after it, not a separately computed answer.
func TestFormatDMSSecondsExtendsFormatDMS(t *testing.T) {
	for deg := 0.0; deg < 30; deg += 0.011 {
		short, long := FormatDMS(deg), FormatDMSSeconds(deg)
		if len(long) <= len(short) || long[:len(short)-1] != short[:len(short)-1] {
			t.Fatalf("FormatDMSSeconds(%v) = %q does not extend %q", deg, long, short)
		}
	}
}

// Both rulership schemes have to be complete and consistent: every sign has a
// ruler in each, the traditional scheme uses only the seven visible planets, and
// the two differ in exactly the three signs the outer planets were given.
func TestRulershipSchemes(t *testing.T) {
	visible := map[string]bool{
		"Sun": true, "Moon": true, "Mercury": true, "Venus": true,
		"Mars": true, "Jupiter": true, "Saturn": true,
	}

	differ := map[Sign]string{Scorpio: "Pluto", Aquarius: "Uranus", Pisces: "Neptune"}
	traditionalCount := map[string]int{}

	for s := Aries; s <= Pisces; s++ {
		trad, modern := s.TraditionalRuler(), s.ModernRuler()
		if trad == "" || modern == "" {
			t.Fatalf("%s has no ruler", s)
		}
		if !visible[trad] {
			t.Errorf("%s: traditional ruler %s is not one of the seven planets", s, trad)
		}
		traditionalCount[trad]++

		if want, ok := differ[s]; ok {
			if modern != want {
				t.Errorf("%s: modern ruler = %s, want %s", s, modern, want)
			}
		} else if modern != trad {
			t.Errorf("%s: modern ruler %s differs from traditional %s unexpectedly", s, modern, trad)
		}
	}

	// The seven planets over twelve signs: the Sun and the Moon take one each and
	// the other five take two.
	for planet, count := range traditionalCount {
		want := 2
		if planet == "Sun" || planet == "Moon" {
			want = 1
		}
		if count != want {
			t.Errorf("%s rules %d signs traditionally, want %d", planet, count, want)
		}
	}

	// Ruler is whichever scheme the application declares it reads under.
	for s := Aries; s <= Pisces; s++ {
		want := s.ModernRuler()
		if Rulership == TraditionalRulership {
			want = s.TraditionalRuler()
		}
		if got := s.Ruler(); got != want {
			t.Errorf("%s: Ruler() = %s, want %s under the %s scheme", s, got, want, Rulership)
		}
	}
}
