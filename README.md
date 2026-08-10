# Birth Chart Calculator

A small web application that casts an astrology birth chart from a date, a time
and a place: the planets, the Ascendant and Descendant, the twelve houses, and
a chart wheel you can read on a phone or print.

Everything it needs is compiled into the binary — the planetary theory, the
time-zone database and a gazetteer of about seventy thousand places — so it
runs with no network access, no API keys and no C toolchain.

```
go run .
```

Then open <http://localhost:8080>.

## Options

| Flag | Default | What it does |
|---|---|---|
| `-addr` | `localhost:8080` | Address to listen on |
| `-open` | off | Open a browser once the server is up |
| `-online` | off | Allow the opt-in online place search |

`-online` adds a button that looks a place up through the [Open-Meteo geocoding
API](https://open-meteo.com/en/docs/geocoding-api) when the built-in gazetteer
does not have it. It is off by default because using it sends the typed place
name to a third party. Nothing else in the application ever makes a network
request.

## When the birthplace is not on file

The gazetteer holds seventy thousand populated places, which is not every
hamlet. A birthplace it does not have can be entered directly, under **Set the
coordinates by hand**: give the latitude and longitude and the place keeps
whatever name and country you typed, whether or not either is on file. Leave
the time zone blank as well and the zone of the nearest place on file is used,
with a warning on the page and in the export naming both the zone and the place
it came from — right in the middle of a country, and capable of being an hour
out near a border, which only the reader can judge.

Typing the address in full works too. Everything after the first comma narrows
the search, one qualifier at a time, and a qualifier the gazetteer has no field
for is ignored rather than fatal: it stores a country and a top-level region
and nothing below, so the province in "San Felipe, Zambales, Philippines"
matches nothing and the country still finds the town.

## What it produces

**A table of every planet plus the Ascendant and Descendant**, with the sign
and degree, what that body governs, what the placement means, and how it tends
to show up. **A table of the twelve houses** with the sign on each cusp, what
that area of life covers, how it tends to show up given that sign, and which
bodies fall in it. **A table of the secondary points** — Midheaven, Imum Coeli
and the two lunar nodes — on the same footing. **A chart wheel** as inline SVG,
downloadable as SVG or as a 2× PNG. **Aspects** between the planets, listed with
their orb and what the pairing tends to look like, and drawn across the wheel.
**The whole report as a PDF**, and **the whole chart as a JSON file** for
loading into something else.

The PDF is the browser's own print output, reached from a **Download as PDF**
button on the result page: it renders the page that is on the screen, so the
wheel stays vector, the text stays selectable and the glyphs come out in the
font that drew them. The alternative — rasterising the page in JavaScript, or
writing the file server-side — costs a megabyte of vendored library for a fuzzy
picture, or a second chart renderer and a font to embed, since the fonts every
PDF reader already has hold no zodiac signs. The one thing it does not do is
save without asking: the print dialog opens, and the destination has to be
*Save as PDF* rather than a printer.

Every reading is specific to the placement rather than generic. There are 288
for the bodies (twelve bodies × twelve signs, a definition and a manifestation
each), 144 for the houses (twelve houses × twelve cusp signs), 48 for the
secondary points, and 264 for the aspects. They live as editable JSON under
`internal/interp/data`, not in Go source. Tests check that none is missing, that
no two are identical, and that a reading never paraphrases the one it will be
shown beside — a house reading against the body in that sign, and a Midheaven or
Imum Coeli reading against the tenth or fourth house, which under Placidus
always carries the same sign.

The aspect readings are 66 pairs of bodies × **four** tones, not five. The trine
and the sextile share one: they differ in strength rather than in kind, both
saying the two bodies cooperate, and a reader never sees both because a given
pair makes at most one aspect in a chart. Writing two versions of the same
sentence would be padding. The square and the opposition are kept apart, because
friction you feel inside yourself and friction that arrives through other people
are different experiences.

## The JSON export

Every result page offers **Download the data as JSON**, and the same document is
served by the API:

```
GET  /chart.json?name=…&date=…&time=…&zone=…&location=…&latitude=…&longitude=…
GET  /api/chart?…
POST /api/chart               (form-encoded, the same fields as the web form)
GET  /birth-chart.schema.json (the JSON Schema the document validates against)
```

`/chart.json` sets a `Content-Disposition` header so a browser saves it;
`/api/chart` is the same bytes without one. Incomplete details come back as
`422` with an `errors` object keyed by field name. Adding `?minorAspects=1` to
either endpoint includes the quincunx, semisextile, semisquare, sesquisquare,
quintile and biquintile, each flagged `"minor": true`; they are off by default
because no reading is written for them and because most software does not list
them.

The document is one object with nine parts:

| Key | What is in it |
|---|---|
| `$schema`, `format`, `formatVersion`, `version` | Where the schema is served, `"birth-chart"`, a semantic version (`2.0.0`), and its major component on its own for a consumer that only needs the generation |
| `generatedAt` | When the file was written. Not a property of the chart: two exports of one birth differ here and nowhere else |
| `generator` | The software and the data: application and engine versions, the ephemerides with their versions, the time-zone database, and how the display strings were formatted |
| `calculationSettings` | The astrological conventions: zodiac, house system, node type, rulership system, observer perspective, coordinate reference, the orb allowed for each aspect, which points aspects were computed between, and the cusp convention |
| `rawInput` | The form exactly as it was typed, so the file can be fed back in to reproduce itself |
| `birth` | The instant local and in UTC, the zone with its offset and whether daylight saving applied, the coordinates, the Julian day in UT and TT, and the local sidereal time |
| `birthDataQuality` | How well the time and place are known: `timeStatus`, its source, the accuracy in seconds, and whether the chart was rectified |
| `calculatedChart` | `angles`, `points`, `derivedPoints`, `houses`, `aspects`, plus `sect`, `moonPhase`, `chartRuler` and `distribution`. Numbers only — no prose |
| `interpretations` | The written readings, keyed to that geometry and versioned apart from it |
| `warnings` | Anything the reader was told about how the input was interpreted, each with a stable `code`, a `severity`, and the input fields it bears on |

`points` holds sixteen — the ten planets, the four angles and the two nodes —
each with its ecliptic longitude, its right ascension and declination, the sign
with element, modality, polarity and both rulers, the degree, arcminute and
arcsecond within the sign, the decan, the house, and the daily motion.
`derivedPoints` holds five more that are computed from the others rather than
observed — Part of Fortune, Black Moon Lilith, Vertex, Anti-Vertex and East
Point — each carrying a `derivation` giving the formula used. `houses` holds the
twelve cusps with the sign on each, its ruler and which bodies fall in it.
`aspects` holds the pair, the aspect, the exact angle it is named for, the
actual separation, the orb, the orb that was allowed, and whether the pair was
still applying.

Six rules shape it, and they are worth knowing before writing a consumer:

- **Calculation and interpretation are separate.** Everything under
  `calculatedChart` is geometry; every sentence of prose is under
  `interpretations`, joined to it by the same keys. A consumer wanting only the
  numbers can drop that whole object, and replacing the readings — or
  translating them — touches nothing else. `interpretations` carries its own
  `locale`, `system` and `interpretationVersion` for that reason.
- **Every angle appears twice**, once as a number and once as the string a
  reader would recognise: `"longitudeDegrees": 334.964358` beside
  `"formatted": "4°57'51\" Pisces"`. Deriving one from the other involves
  truncated arcseconds and sign boundaries, and two consumers would do it two
  ways. Positions carry arcseconds, and `generator.formatting` states that they
  are truncated rather than rounded — rounding `29°59'40"` would give `30°00'`,
  a degree belonging to the next sign.
- **Anything not known is explicitly `null`, not absent.** The chart holds no
  rate of motion for the four angles or the derived points, so their
  `speedPerDay` is `null` and `applying` on an aspect to an angle is `null`
  — which is a third answer, distinct from `true` and `false`, and a consumer
  cannot mistake it for the angles standing still. Empty collections are `[]`,
  never `null`.
- **Every convention is stated rather than assumed.** The lunar nodes are the
  **true** nodes, Lilith is the **mean** apogee, rulerships are **modern** with
  the traditional ruler given alongside, and the perspective is geocentric —
  all of it in `calculationSettings`, because two programs disagreeing on any
  of these produce different charts from the same birth with nothing in the
  file to show why.
- **The tables join on one key.** A house's `occupants`, an aspect's `a` and
  `b`, the chart ruler's `rulerKey`, and the keys of `interpretations.points`
  and `interpretations.aspects` are all the same camelCase identifier as a
  point's `key` or an aspect's `id`, so nothing has to be matched on a display
  name.
- **Nothing that changes on its own is stored.** There is no age, and no
  "current" anything: a file that silently goes stale is worse than one that
  omits the field.

The Moon's phase is named twice for a related reason. `name` is the almanac
name, which always agrees with `illumination`; `lunationPhase` is the eighth of
the cycle that natal astrology reads, so a Moon 83% lit and waning is a
`"Waning Gibbous"` and a `"Disseminating"` Moon at once.

Two things the document cannot honestly fill in, and says so with `null`:
`generator.timeZoneDatabase.version`, because neither `time/tzdata` nor a
host's own zoneinfo files publish a version at run time and a guess would be
worse than nothing; and `birthDataQuality.locationAccuracyMetres`, because the
gazetteer gives one named point per place and not an area — a `locationSource`
string says where the coordinates came from instead.

The schema is served at `/birth-chart.schema.json` and embedded in the binary.
It is checked in both directions by the test suite: every field a document
produces must be described by the schema, and every property the schema
declares must be produced by some document, so neither can quietly drift from
the other.

## Accuracy

Positions come from the full **VSOP87D** planetary theory (Bretagnon & Francou,
1988), evaluated directly from the published coefficient files, with light-time
iteration and the FK5, aberration and nutation corrections of Meeus chapter 33.
The Moon uses the truncated ELP-2000/82 series of Meeus chapter 47 and Pluto
the series of chapter 37, precessed from J2000 to the equinox of date. Houses
are **Placidus**, solved by iteration on the ascensional difference, falling
back to Whole Sign inside the polar circles where Placidus has no solution.

The results are checked against independent references in the test suite:

- the official `vsop87.chk` reference values, matched to better than 0.001″;
- Meeus's worked examples for the Sun, Venus and the Moon;
- **Swiss Ephemeris 2.10.03** for planetary longitudes and for all twelve
  Placidus cusps across six charts from the equator to 64° north, in both
  hemispheres, from 1879 to 2001.

A wider sweep — sixty random births between 1900 and 2025 across ten places
from the equator to 64° north, in both hemispheres — agrees with Swiss
Ephemeris to within 1.8″ on the planets, 10″ on the Moon and 1″ on every house
cusp, with no sign or house placement differing at all.

## Time zones

The hour of birth is the input a reader cannot check from the output. An hour's
error barely moves the planets but swings the Ascendant about fifteen degrees
and takes every house cusp with it, so a wrong zone produces a chart that looks
entirely plausible and is entirely wrong. The application therefore treats the
zone as something to be established rather than assumed.

Historical zones come from the IANA database that `time/tzdata` embeds, so the
offset used is the one actually in force on the day — daylight saving is applied
only in the years a place observed it. Adelaide, for example, kept daylight
saving during the war, dropped it from 1945 until 1971, and has kept it since;
a February 1962 birth there is cast at UTC+09:30 and a February 1972 one at
UTC+10:30. This matters more than it sounds: several tooling stacks ship the
trimmed database in which zones that have agreed since 1970 are merged, so
`Atlantic/Reykjavik` becomes an alias for `Africa/Abidjan` and a 1905 Icelandic
birth silently loses its real −01:28 offset. Go's copy keeps the pre-1970
history.

Three safeguards sit on top of that:

- **No zone is pre-filled.** The zone belongs to the birthplace, not to whoever
  is running the server, and a pre-filled value is indistinguishable from a
  deliberate choice. Left blank, the birthplace's own zone is used — or, for
  coordinates typed by hand, the zone of the nearest place on file, which the
  chart then says it did.
- **A chosen zone is checked against the coordinates.** If it is not the zone of
  any of the twelve nearest places in the gazetteer, the page says so and names
  the zone it expected, along with both offsets.
- **The offset used is stated on the result page**, together with whether
  daylight saving was in force, so the decision is visible rather than implied.

A birth time that falls inside a daylight-saving change — one that never
happened, or that happened twice — is flagged rather than silently resolved.

The form also takes, optionally, **how well the time is known** — recorded,
approximate, rectified or unknown — and where it came from. It changes none of
the arithmetic. It is recorded because the angles and the cusps move a degree
every four minutes, so a chart cast on a remembered time is a different sort of
object from one cast on a certificate, and once the file leaves the page nothing
else says which it was. An approximate, unknown or rectified time raises a
warning on the page and in the export; a rectified chart in particular is a
hypothesis about the birth time rather than a record of it, and a consumer that
cannot tell the two apart will treat them alike.

## Layout

```
main.go                  server bootstrap and flags
internal/astro/          the ephemeris, houses, and chart assembly
internal/interp/         the written meanings, one JSON file per body
internal/places/         the gazetteer and the time-zone list
internal/wheel/          the SVG chart wheel
internal/web/            handlers, templates, CSS and JavaScript
```

The front end is server-rendered HTML with a little vanilla JavaScript. The
searchable dropdowns and the PNG and PDF exports are progressive enhancements;
the form works without them, and Ctrl+P produces the same document the PDF
button does.

## Regenerating the place data

`internal/places/data/cities.tsv.gz` and `internal/places/zones_gen.go` are
committed, so an ordinary build needs no network. To refresh them from upstream:

```
go run ./internal/places/gen
```

## Data and licences

Place data © [GeoNames](https://www.geonames.org/), CC BY 4.0. The VSOP87
coefficient files are the freely distributed tables of the Bureau des
Longitudes. Lunar, Pluto, nutation and sidereal-time routines come from
[github.com/soniakeys/meeus](https://github.com/soniakeys/meeus) (MIT).
