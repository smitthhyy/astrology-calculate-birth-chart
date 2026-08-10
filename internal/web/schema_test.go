package web

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"testing"
	_ "time/tzdata"
)

// A published schema that has drifted from the document is worse than no schema
// at all: a consumer validates against it, passes, and then finds the field it
// wanted missing — or, more likely, generates its types from the schema and gets
// a compile error against real data. There is no JSON Schema library here, so
// these tests walk the two against each other directly, covering the subset of
// the vocabulary the schema uses: $ref, oneOf, type, required, properties,
// additionalProperties, items, enum, const, and the numeric bounds.
//
// The check runs both ways, because each direction catches a different mistake.
// Validating a document against the schema catches a field added to the export
// and never written down. Requiring that every declared property be exercised by
// some document catches a field renamed in the export and left behind in the
// schema, which validation alone would pass in silence.

func TestSchemaIsServed(t *testing.T) {
	rec := get(t, newTestServer(t, false), schemaPath)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "json") {
		t.Errorf("content type %q", ct)
	}

	var schema map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &schema); err != nil {
		t.Fatalf("the served schema is not valid JSON: %v", err)
	}
	// Every document points at this path, so the path has to be the one served.
	if id, _ := schema["$id"].(string); id != schemaPath {
		t.Errorf("the schema's $id is %q but it is served from %q", id, schemaPath)
	}
}

// TestExportMatchesItsSchema validates real documents against the schema they
// declare themselves against.
func TestExportMatchesItsSchema(t *testing.T) {
	for name, mutate := range formVariants() {
		t.Run(name, func(t *testing.T) {
			form := referenceForm()
			mutate(form)
			_, raw := exportFor(t, form.Encode())

			v := newSchemaChecker(t)
			v.check(raw, v.schema, "", "#")
		})
	}
}

// TestSchemaDeclaresNothingTheExportOmits is the other direction: every property
// the schema describes must turn up in some document. One that never does is
// either a field that has been renamed or one that was never built, and either
// way a consumer generating code from the schema will be expecting it.
func TestSchemaDeclaresNothingTheExportOmits(t *testing.T) {
	v := newSchemaChecker(t)
	for _, mutate := range formVariants() {
		form := referenceForm()
		mutate(form)
		_, raw := exportFor(t, form.Encode())
		v.check(raw, v.schema, "", "#")
	}

	var missing []string
	v.eachDeclaredProperty(v.schema, "#", map[string]bool{}, func(path string) {
		if !v.seen[path] {
			missing = append(missing, path)
		}
	})
	sort.Strings(missing)
	for _, path := range missing {
		t.Errorf("the schema declares %s, which no exported document contains", path)
	}
}

// formVariants are the inputs needed to reach every branch of the document
// between them. Several fields only appear under particular conditions: a
// mismatched zone produces warnings, the minor aspects add rules, a stated
// source fills in birthDataQuality, and the far north changes the house system.
func formVariants() map[string]func(form url.Values) {
	return map[string]func(form url.Values){
		"the reference chart": func(url.Values) {},
		"with minor aspects": func(form url.Values) {
			form.Set("minorAspects", "1")
		},
		"with the birth data described": func(form url.Values) {
			form.Set("timeStatus", "recorded")
			form.Set("timeSource", "birth certificate")
		},
		"with a doubtful zone": func(form url.Values) {
			form.Set("zone", "America/New_York")
		},
		// A birthplace the gazetteer does not hold, given by coordinates alone,
		// which is the only route to an inferred time zone.
		"with coordinates entered by hand": func(form url.Values) {
			form.Set("location", "San Felipe, Zambales")
			form.Set("country", "Philippines")
			form.Set("latitude", "15.0619")
			form.Set("longitude", "120.0742")
			form.Del("zone")
		},
		// Inside the polar circles Placidus has no solution, which is the one
		// input that changes the house system and the warnings together.
		"inside the arctic circle": func(form url.Values) {
			form.Set("location", "Tromso")
			form.Set("country", "NO")
			form.Set("latitude", "69.6489")
			form.Set("longitude", "18.9551")
			form.Set("zone", "Europe/Oslo")
		},
	}
}

// schemaChecker validates documents against the schema and records which of its
// declared properties they exercised.
type schemaChecker struct {
	t      *testing.T
	schema map[string]any
	// seen holds the location within the schema of every property some document
	// carried — "#/$defs/point/properties/decan" and so on. Keying on the schema
	// rather than on the document matters: a shared definition used in two places
	// is one thing to have got wrong, and a field that legitimately appears in
	// only one of those places should not read as missing from the other.
	seen map[string]bool
}

func newSchemaChecker(t *testing.T) *schemaChecker {
	t.Helper()
	var schema map[string]any
	if err := json.Unmarshal(schemaJSON, &schema); err != nil {
		t.Fatalf("the embedded schema is not valid JSON: %v", err)
	}
	return &schemaChecker{t: t, schema: schema, seen: map[string]bool{}}
}

// resolve follows a local $ref. Only "#/$defs/name" is used here, which is all
// the schema is written with.
func (v *schemaChecker) resolve(ref string) map[string]any {
	v.t.Helper()
	name := strings.TrimPrefix(ref, "#/$defs/")
	defs, _ := v.schema["$defs"].(map[string]any)
	target, ok := defs[name].(map[string]any)
	if !ok {
		v.t.Fatalf("the schema refers to %q, which it does not define", ref)
	}
	return target
}

// check validates one value. doc is where it sits in the document, for the error
// message; where is where its schema sits, for the drift bookkeeping.
func (v *schemaChecker) check(value any, node map[string]any, doc, where string) {
	// A $ref beside a description is how the schema annotates a shared
	// definition at its point of use. The referenced schema still governs.
	if ref, ok := node["$ref"].(string); ok {
		node, where = v.resolve(ref), ref
	}

	if branches, ok := node["oneOf"].([]any); ok {
		v.checkOneOf(value, branches, doc, where)
		return
	}
	v.checkType(value, node, doc)
	v.checkEnum(value, node, doc)

	switch typed := value.(type) {
	case map[string]any:
		v.checkObject(typed, node, doc, where)
	case []any:
		items, ok := node["items"].(map[string]any)
		if !ok {
			return
		}
		for i, item := range typed {
			v.check(item, items, fmt.Sprintf("%s[%d]", doc, i), where+"/items")
		}
	case float64:
		v.checkNumber(typed, node, doc)
	}
}

// checkOneOf passes if any branch does. Every use of it in this schema is a
// nullable object, so a null value satisfies the null branch and anything else
// is held against the other one.
func (v *schemaChecker) checkOneOf(value any, branches []any, doc, where string) {
	for i, branch := range branches {
		object, ok := branch.(map[string]any)
		if !ok {
			continue
		}
		if object["type"] == "null" {
			if value == nil {
				return
			}
			continue
		}
		if value != nil {
			v.check(value, object, doc, fmt.Sprintf("%s/oneOf/%d", where, i))
			return
		}
	}
	if value != nil {
		v.t.Errorf("%s: a %s matches none of the alternatives", doc, jsonTypeOf(value))
	}
}

func (v *schemaChecker) checkObject(value map[string]any, node map[string]any, doc, where string) {
	properties, _ := node["properties"].(map[string]any)
	required, hasRequired := node["required"].([]any)
	additional, hasAdditional := node["additionalProperties"]

	for _, entry := range required {
		name, ok := entry.(string)
		if !ok {
			continue
		}
		if _, present := value[name]; !present {
			v.t.Errorf("%s: the schema requires %q, which the document does not contain", doc, name)
		}
	}
	// A missing required list is its own kind of drift: a field declared and not
	// required reads to a consumer as optional, and everything in this document
	// is either present or explicitly null. Where an object genuinely has no
	// mandatory field the schema says so with an empty list, which is a
	// statement rather than an omission.
	if len(properties) > 0 && !hasRequired {
		v.t.Errorf("%s: the schema declares properties but has no required list; "+
			"write an explicit empty one if nothing is in fact mandatory", where)
	}

	for name, child := range value {
		childDoc := join(doc, name)
		if declared, ok := properties[name].(map[string]any); ok {
			childWhere := where + "/properties/" + name
			v.seen[childWhere] = true
			v.check(child, declared, childDoc, childWhere)
			continue
		}
		// A free-form map — the readings, the distribution counts — is described
		// by one schema standing for all its values.
		if schema, ok := additional.(map[string]any); ok {
			childWhere := where + "/additionalProperties"
			v.seen[childWhere] = true
			v.check(child, schema, childDoc, childWhere)
			continue
		}
		if hasAdditional && additional == false {
			v.t.Errorf("%s: the document has %q, which the schema does not describe", doc, name)
		}
	}
}

func (v *schemaChecker) checkType(value any, node map[string]any, doc string) {
	allowed := typeNames(node["type"])
	if len(allowed) == 0 {
		return
	}
	got := jsonTypeOf(value)
	for _, name := range allowed {
		// JSON has one numeric type; the schema distinguishes an integer from a
		// number, and a whole-valued number satisfies either.
		if name == got || (name == "number" && got == "integer") {
			return
		}
	}
	v.t.Errorf("%s: the document has a %s, the schema allows %v", doc, got, allowed)
}

func (v *schemaChecker) checkEnum(value any, node map[string]any, doc string) {
	if constant, ok := node["const"]; ok && value != constant {
		v.t.Errorf("%s: %v, want the constant %v", doc, value, constant)
	}
	values, ok := node["enum"].([]any)
	if !ok {
		return
	}
	for _, candidate := range values {
		if candidate == value {
			return
		}
	}
	v.t.Errorf("%s: %v is not one of %v", doc, value, values)
}

// checkNumber holds a value against the bounds. These are the assertions that
// catch a calculation bug rather than a documentation slip — a longitude at 360°,
// a declination past the pole, an arcsecond that rolled over instead of carrying.
func (v *schemaChecker) checkNumber(value float64, node map[string]any, doc string) {
	if limit, ok := node["minimum"].(float64); ok && value < limit {
		v.t.Errorf("%s: %v is below the minimum %v", doc, value, limit)
	}
	if limit, ok := node["maximum"].(float64); ok && value > limit {
		v.t.Errorf("%s: %v is above the maximum %v", doc, value, limit)
	}
	if limit, ok := node["exclusiveMaximum"].(float64); ok && value >= limit {
		v.t.Errorf("%s: %v is not below the exclusive maximum %v", doc, value, limit)
	}
	if limit, ok := node["exclusiveMinimum"].(float64); ok && value <= limit {
		v.t.Errorf("%s: %v is not above the exclusive minimum %v", doc, value, limit)
	}
}

// eachDeclaredProperty walks every property the schema describes, yielding the
// same location strings check records in seen. A definition already on the stack
// is skipped: the schema is not recursive, and following one that was would not
// terminate.
func (v *schemaChecker) eachDeclaredProperty(node map[string]any, where string, visiting map[string]bool, yield func(string)) {
	if ref, ok := node["$ref"].(string); ok {
		if visiting[ref] {
			return
		}
		visiting[ref] = true
		defer delete(visiting, ref)
		node, where = v.resolve(ref), ref
	}

	if branches, ok := node["oneOf"].([]any); ok {
		for i, branch := range branches {
			object, ok := branch.(map[string]any)
			if ok && object["type"] != "null" {
				v.eachDeclaredProperty(object, fmt.Sprintf("%s/oneOf/%d", where, i), visiting, yield)
			}
		}
		return
	}
	if items, ok := node["items"].(map[string]any); ok {
		v.eachDeclaredProperty(items, where+"/items", visiting, yield)
		return
	}
	if additional, ok := node["additionalProperties"].(map[string]any); ok {
		// A free-form map no document ever fills is as much a drift as a missing
		// property, so its stand-in schema is yielded too.
		yield(where + "/additionalProperties")
		v.eachDeclaredProperty(additional, where+"/additionalProperties", visiting, yield)
	}
	properties, ok := node["properties"].(map[string]any)
	if !ok {
		return
	}
	for name, child := range properties {
		object, ok := child.(map[string]any)
		if !ok {
			continue
		}
		childWhere := where + "/properties/" + name
		yield(childWhere)
		v.eachDeclaredProperty(object, childWhere, visiting, yield)
	}
}

func join(path, name string) string {
	if path == "" {
		return name
	}
	return path + "." + name
}

func typeNames(node any) []string {
	switch typed := node.(type) {
	case string:
		return []string{typed}
	case []any:
		out := make([]string, 0, len(typed))
		for _, entry := range typed {
			if name, ok := entry.(string); ok {
				out = append(out, name)
			}
		}
		return out
	}
	return nil
}

func jsonTypeOf(value any) string {
	switch typed := value.(type) {
	case nil:
		return "null"
	case bool:
		return "boolean"
	case string:
		return "string"
	case float64:
		if typed == math.Trunc(typed) {
			return "integer"
		}
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	}
	return fmt.Sprintf("%T", value)
}
