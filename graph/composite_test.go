package graph

import (
	"slices"
	"testing"

	"github.com/hanzoai/base/core"
)

// Every composite field type's parts are the graph's, in the field's order, and
// contains reads text where the field says it has text to read.
func TestCompositesAreTheFieldsOwn(t *testing.T) {
	t.Parallel()

	for ft, c := range composites {
		f := core.Fields[ft]()
		field, ok := f.(core.CompositeField)
		if !ok {
			t.Fatalf("%s is not a composite", ft)
		}
		var names []string
		for _, p := range c.parts {
			names = append(names, p.name)
		}
		if !slices.Equal(names, field.Parts()) {
			t.Errorf("%s: the graph names %v, the field %v", ft, names, field.Parts())
		}
		if _, text := f.(core.TextParts); text != c.text {
			t.Errorf("%s: contains reads text in the graph %v, in the field %v", ft, c.text, text)
		}
	}
	for ft, factory := range core.Fields {
		if core.IsComposite(factory()) && composites[ft] == nil {
			t.Errorf("the graph has no shape for the composite %s", ft)
		}
	}
}
