package graph

import "github.com/hanzoai/base/core"

// composite is the shape of a composite field's value: the object it answers,
// the parts a filter, a sort and an input name, in the order its field names
// them (graph_test holds the two to each other).
type composite struct {
	typ   string // FullName: BaseFullName, BaseFullNameFilter, …
	parts []part
	write bool // takes a value in an Input; an actor is Base's to stamp
	text  bool // contains reads its text parts (the field is a core.TextParts)
}

// part is one named part of a composite value.
type part struct {
	name string
	kind kind    // kText or kNumber; a list part is kJSON (only contains)
	list bool    // a list of values: answered whole, filtered by contains
	elem *object // the object a list part holds, nil for a list of strings
}

// object is an object inside a composite part: a phone, a link.
type object struct {
	typ    string
	fields []string // every one a String
}

var (
	phone = &object{typ: "Phone", fields: []string{"number", "countryCode", "callingCode"}}
	link  = &object{typ: "Link", fields: []string{"url", "label"}}
)

func text(name string) part   { return part{name: name, kind: kText} }
func number(name string) part { return part{name: name, kind: kNumber} }

// composites is every composite field type, by the field type it shapes.
var composites = map[string]*composite{
	core.FieldTypeCurrency: {typ: "Currency", write: true, parts: []part{
		number("amountMicros"), text("currencyCode"),
	}},
	core.FieldTypeEmails: {typ: "Emails", write: true, text: true, parts: []part{
		text("primaryEmail"), {name: "additionalEmails", kind: kJSON, list: true},
	}},
	core.FieldTypePhones: {typ: "Phones", write: true, text: true, parts: []part{
		text("primaryPhoneNumber"), text("primaryPhoneCountryCode"), text("primaryPhoneCallingCode"),
		{name: "additionalPhones", kind: kJSON, list: true, elem: phone},
	}},
	core.FieldTypeLinks: {typ: "Links", write: true, text: true, parts: []part{
		text("primaryLinkUrl"), text("primaryLinkLabel"),
		{name: "secondaryLinks", kind: kJSON, list: true, elem: link},
	}},
	core.FieldTypeAddress: {typ: "Address", write: true, text: true, parts: []part{
		text("addressStreet1"), text("addressStreet2"), text("addressCity"),
		text("addressState"), text("addressPostcode"), text("addressCountry"),
		number("addressLat"), number("addressLng"),
	}},
	core.FieldTypeFullName: {typ: "FullName", write: true, text: true, parts: []part{
		text("firstName"), text("lastName"),
	}},
	core.FieldTypeActor: {typ: "Actor", text: true, parts: []part{
		text("source"), text("id"), text("name"),
	}},
}

func (c *composite) part(name string) *part {
	for i := range c.parts {
		if c.parts[i].name == name {
			return &c.parts[i]
		}
	}
	return nil
}

func (c *composite) sortable() bool {
	for _, p := range c.parts {
		if !p.list {
			return true
		}
	}
	return false
}
