package astro

import "math"

// deltaT returns TT − UT in seconds for the given decimal year, using the
// piecewise polynomials published by Espenak & Meeus (NASA GSFC eclipse site).
//
// meeus/v3/deltat is not used here: its interpolation table stops at 2010 and
// its post-2000 polynomial is a long-term fit that reads ~35 s high for the
// 2020s. The expressions below track the observed record to within a couple of
// seconds over the range birth dates actually fall in, which is far finer than
// this chart needs — even the Moon, the fastest body, moves only 0.55″ per
// second of time.
func deltaT(year float64) float64 {
	switch {
	case year < 1600:
		u := (year - 1820) / 100
		return -20 + 32*u*u

	case year < 1700:
		t := year - 1600
		return horner(t, 120, -0.9808, -0.01532, 1.0/7129)

	case year < 1800:
		t := year - 1700
		return horner(t, 8.83, 0.1603, -0.0059285, 0.00013336, -1.0/1174000)

	case year < 1860:
		t := year - 1800
		return horner(t, 13.72, -0.332447, 0.0068612, 0.0041116, -0.00037436,
			0.0000121272, -0.0000001699, 0.000000000875)

	case year < 1900:
		t := year - 1860
		return horner(t, 7.62, 0.5737, -0.251754, 0.01680668, -0.0004473624, 1.0/233174)

	case year < 1920:
		t := year - 1900
		return horner(t, -2.79, 1.494119, -0.0598939, 0.0061966, -0.000197)

	case year < 1941:
		t := year - 1920
		return horner(t, 21.20, 0.84493, -0.076100, 0.0020936)

	case year < 1961:
		t := year - 1950
		return horner(t, 29.07, 0.407, -1.0/233, 1.0/2547)

	case year < 1986:
		t := year - 1975
		return horner(t, 45.45, 1.067, -1.0/260, -1.0/718)

	case year < 2005:
		t := year - 2000
		return horner(t, 63.86, 0.3345, -0.060374, 0.0017275, 0.000651814, 0.00002373599)

	case year < 2050:
		t := year - 2000
		return horner(t, 62.92, 0.32217, 0.005589)

	case year < 2150:
		u := (year - 1820) / 100
		return -20 + 32*u*u - 0.5628*(2150-year)

	default:
		u := (year - 1820) / 100
		return -20 + 32*u*u
	}
}

// horner evaluates c[0] + c[1]x + c[2]x² + ...
func horner(x float64, c ...float64) float64 {
	sum := 0.0
	for i := len(c) - 1; i >= 0; i-- {
		sum = sum*x + c[i]
	}
	return sum
}

// decimalYear converts a Julian Day to a decimal calendar year, close enough
// for selecting and evaluating a ΔT polynomial.
func decimalYear(jd float64) float64 {
	return 2000 + (jd-j2000)/365.25
}

// jdeFromUT converts a Julian Day in Universal Time to Julian Ephemeris Day
// (Terrestrial Time), which is the argument every ephemeris in this package
// expects.
func jdeFromUT(jdUT float64) float64 {
	return jdUT + deltaT(decimalYear(jdUT))/86400
}

// julianDay returns the Julian Day for a Gregorian calendar date and a
// fraction-of-day, following Meeus chapter 7.
func julianDay(year, month int, day float64) float64 {
	if month <= 2 {
		year--
		month += 12
	}
	a := math.Floor(float64(year) / 100)
	b := 2 - a + math.Floor(a/4)
	return math.Floor(365.25*float64(year+4716)) +
		math.Floor(30.6001*float64(month+1)) + day + b - 1524.5
}
