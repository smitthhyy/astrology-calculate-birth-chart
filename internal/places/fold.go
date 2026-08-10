package places

import (
	"strings"
	"sync"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// foldPool hands out diacritic-stripping transformers. They are stateful and
// so cannot be shared between the concurrent requests a web server produces.
var foldPool = sync.Pool{
	New: func() any {
		return transform.Chain(
			norm.NFD,                           // split é into e + combining acute
			runes.Remove(runes.In(unicode.Mn)), // drop the combining marks
			norm.NFC,
		)
	},
}

// fold reduces a place name to a form that can be matched against what someone
// is likely to type: lower case, with accents removed. It is what lets
// "zurich" find "Zürich" and "sao paulo" find "São Paulo", which the
// gazetteer's own ASCII column does not — GeoNames transliterates Zürich as
// "Zuerich".
func fold(s string) string {
	lower := strings.ToLower(s)
	t := foldPool.Get().(transform.Transformer)
	defer foldPool.Put(t)

	folded, _, err := transform.String(t, lower)
	if err != nil {
		return lower // an unfoldable name is still searchable as-is
	}
	return folded
}
