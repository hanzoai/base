package core_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tests"
	"github.com/hanzoai/base/tools/types"
	"github.com/hanzoai/dbx"
)

// errorCodes flattens a validation error into "path:code" lines, which is what
// a scenario asserts on (the messages are prose and may change).
func errorCodes(err error) string {
	var b strings.Builder
	var walk func(prefix string, err error)
	walk = func(prefix string, err error) {
		switch e := err.(type) {
		case validation.Errors:
			for k, v := range e {
				walk(prefix+"."+k, v)
			}
		case validation.Error:
			b.WriteString(prefix + ":" + e.Code() + "\n")
		default:
			b.WriteString(prefix + ":" + err.Error() + "\n")
		}
	}
	walk("", err)
	return b.String()
}

func TestCompositeFieldBaseMethods(t *testing.T) {
	for _, typ := range []string{
		core.FieldTypeCurrency, core.FieldTypeEmails, core.FieldTypePhones, core.FieldTypeLinks,
		core.FieldTypeAddress, core.FieldTypeFullName, core.FieldTypeRating, core.FieldTypeActor,
	} {
		t.Run(typ, func(t *testing.T) { testFieldBaseMethods(t, typ) })
	}
}

// crmCollection is one collection holding every new field type, saved into app.
func crmCollection(t *testing.T, app core.App) *core.Collection {
	t.Helper()

	c := core.NewBaseCollection("contacts")
	c.Fields.Add(
		&core.FullNameField{Name: "name", FieldMeta: core.FieldMeta{Label: "Name", Icon: "IconUser", Position: 1}},
		&core.EmailsField{Name: "emails", Unique: true, MaxValues: 3},
		&core.PhonesField{Name: "phones", DefaultCountry: "US"},
		&core.LinksField{Name: "linkedin", Unique: true},
		&core.AddressField{Name: "address"},
		&core.CurrencyField{Name: "arr", DefaultCurrency: "EUR"},
		&core.RatingField{Name: "rating"},
		&core.SelectField{
			Name: "stage", MaxSelect: 1, Values: []string{"new", "won"},
			Options: []core.SelectOption{{Value: "new", Label: "New", Color: "red"}, {Value: "won", Label: "Won", Color: "green", Position: 1}},
		},
		&core.EditorField{Name: "notes"},
		&core.ActorField{Name: "createdBy", OnCreate: true},
		&core.ActorField{Name: "updatedBy", OnCreate: true, OnUpdate: true},
	)
	if err := app.Save(c); err != nil {
		t.Fatalf("save collection: %v", err)
	}
	return c
}

func TestCompositeValuesRoundTrip(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	c := crmCollection(t, app)

	r := core.NewRecord(c)
	r.Set("name", map[string]any{"firstName": "  Ada ", "lastName": "Lovelace"})
	r.Set("emails", map[string]any{"primaryEmail": "", "additionalEmails": []string{" Ada@Example.COM", "ada@example.com", "", "ada@work.example"}})
	r.Set("phones", map[string]any{"primaryPhoneNumber": "(415) 555-0100", "primaryPhoneCallingCode": "1"})
	r.Set("linkedin", map[string]any{"primaryLinkUrl": "linkedin.com/in/ada"})
	r.Set("address", map[string]any{"addressCity": " London ", "addressCountry": "GB", "addressLat": 51.5})
	r.Set("arr", map[string]any{"amountMicros": 1500000})
	r.Set("rating", "4")
	r.Set("notes", `<p onclick="steal()">hi <script>alert(1)</script><a href="https://x.test">x</a></p>`)
	if err := app.Save(r); err != nil {
		t.Fatalf("save record: %v", err)
	}

	got, err := app.FindRecordById(c, r.Id)
	if err != nil {
		t.Fatal(err)
	}

	checks := map[string]string{
		"name":     `{"firstName":"Ada","lastName":"Lovelace"}`,
		"emails":   `{"primaryEmail":"ada@example.com","additionalEmails":["ada@work.example"]}`,
		"phones":   `{"primaryPhoneNumber":"4155550100","primaryPhoneCountryCode":"US","primaryPhoneCallingCode":"+1","additionalPhones":[]}`,
		"linkedin": `{"primaryLinkUrl":"https://linkedin.com/in/ada","primaryLinkLabel":"","secondaryLinks":[]}`,
		"address":  `{"addressStreet1":"","addressStreet2":"","addressCity":"London","addressState":"","addressPostcode":"","addressCountry":"GB","addressLat":51.5,"addressLng":null}`,
		"arr":      `{"amountMicros":1500000,"currencyCode":"EUR"}`,
		"rating":   `4`,
	}
	for name, want := range checks {
		raw, _ := json.Marshal(got.Get(name))
		if string(raw) != want {
			t.Errorf("%s:\nwant %s\n got %s", name, want, raw)
		}
	}

	notes := got.GetString("notes")
	if strings.Contains(notes, "script") || strings.Contains(notes, "onclick") || !strings.Contains(notes, `href="https://x.test"`) {
		t.Errorf("notes were not sanitized to the policy: %s", notes)
	}

	// select options travel with the schema
	stage, _ := got.Collection().Fields.GetByName("stage").(*core.SelectField)
	if len(stage.Options) != 2 || stage.Options[1].Color != "green" {
		t.Errorf("options did not survive the save: %+v", stage.Options)
	}
	if name := got.Collection().Fields.GetByName("name").(*core.FullNameField); name.Label != "Name" || name.Icon != "IconUser" || name.Position != 1 {
		t.Errorf("field meta did not survive the save: %+v", name.FieldMeta)
	}
}

func TestCompositeEmptyIsNull(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	c := crmCollection(t, app)

	r := core.NewRecord(c)
	r.Set("emails", map[string]any{"primaryEmail": "  ", "additionalEmails": []string{""}})
	if err := app.Save(r); err != nil {
		t.Fatal(err)
	}

	var stored struct {
		Emails *string `db:"emails"`
		Arr    *string `db:"arr"`
	}
	if err := app.DB().Select("emails", "arr").From("contacts").Where(dbx.HashExp{"id": r.Id}).One(&stored); err != nil {
		t.Fatal(err)
	}
	if stored.Emails != nil || stored.Arr != nil {
		t.Fatalf("an empty composite must be stored as NULL, got emails=%v arr=%v", stored.Emails, stored.Arr)
	}

	raw, _ := json.Marshal(r.Get("emails"))
	if string(raw) != "null" {
		t.Fatalf("an empty composite must read as null, got %s", raw)
	}

	found, err := app.FindRecordsByFilter(c, "emails = null", "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 {
		t.Fatalf("expected the empty value to match `emails = null`, got %d", len(found))
	}
}

func TestCompositeValidation(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	c := crmCollection(t, app)

	first := core.NewRecord(c)
	first.Set("emails", types.Emails{PrimaryEmail: "taken@example.com"})
	first.Set("linkedin", types.Links{PrimaryLinkUrl: "https://linkedin.com/in/taken"})
	if err := app.Save(first); err != nil {
		t.Fatal(err)
	}

	scenarios := []struct {
		name  string
		field string
		value any
		code  string
	}{
		{"bad email", "emails", types.Emails{PrimaryEmail: "nope"}, "validation_is_email"},
		{"too many emails", "emails", types.Emails{PrimaryEmail: "a@x.test", AdditionalEmails: []string{"b@x.test", "c@x.test", "d@x.test"}}, "validation_too_many_values"},
		{"primary email taken, any case", "emails", types.Emails{PrimaryEmail: "TAKEN@example.com"}, "validation_not_unique"},
		{"bad phone", "phones", types.Phones{PrimaryPhoneNumber: "12"}, "validation_invalid_phone"},
		{"bad country", "phones", types.Phones{PrimaryPhoneNumber: "4155550100", PrimaryPhoneCountryCode: "USA"}, "validation_invalid_country_code"},
		{"bad calling code", "phones", types.Phones{PrimaryPhoneNumber: "4155550100", PrimaryPhoneCallingCode: "+12345"}, "validation_invalid_calling_code"},
		{"bad url scheme", "linkedin", types.Links{PrimaryLinkUrl: "ftp://x.test"}, "validation_invalid_url"},
		{"primary url taken", "linkedin", types.Links{PrimaryLinkUrl: "https://linkedin.com/in/taken"}, "validation_not_unique"},
		{"unknown currency", "arr", types.Currency{AmountMicros: 1, CurrencyCode: "ZZZ"}, "validation_currency_code"},
		{"rating above max", "rating", 6, "validation_rating_range"},
		{"latitude out of range", "address", map[string]any{"addressLat": 91}, "validation_max_less_equal_than_required"},
		{"name too long", "name", types.FullName{FirstName: strings.Repeat("a", 256)}, "validation_length_too_long"},
	}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			r := core.NewRecord(c)
			r.Set(s.field, s.value)
			err := app.Validate(r)
			if err == nil {
				t.Fatalf("expected a validation error")
			}
			if codes := errorCodes(err); !strings.Contains(codes, s.code) {
				t.Fatalf("expected %q in %s", s.code, codes)
			}
		})
	}
}

func TestCompositePartsFilterAndSort(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	c := crmCollection(t, app)

	for _, p := range []struct {
		first, last, email string
		micros             int64
		code               string
	}{
		{"Ada", "Lovelace", "ada@x.test", 2_000_000, "USD"},
		{"Grace", "Hopper", "grace@x.test", 5_000_000, "EUR"},
		{"Alan", "Turing", "alan@y.test", 1_000_000, "USD"},
	} {
		r := core.NewRecord(c)
		r.Set("name", types.FullName{FirstName: p.first, LastName: p.last})
		r.Set("emails", types.Emails{PrimaryEmail: p.email})
		r.Set("arr", types.Currency{AmountMicros: p.micros, CurrencyCode: p.code})
		if err := app.Save(r); err != nil {
			t.Fatal(err)
		}
	}

	scenarios := []struct {
		filter, sort string
		want         []string // first names, in order
	}{
		{`emails.primaryEmail ~ "x.test"`, "name.lastName", []string{"Grace", "Ada"}},
		{`arr.amountMicros >= 2000000`, "-arr.amountMicros", []string{"Grace", "Ada"}},
		{`arr.currencyCode = "USD"`, "name.firstName", []string{"Ada", "Alan"}},
		{`name.lastName = "Turing"`, "", []string{"Alan"}},
	}

	for _, s := range scenarios {
		t.Run(s.filter, func(t *testing.T) {
			found, err := app.FindRecordsByFilter(c, s.filter, s.sort, 0, 0)
			if err != nil {
				t.Fatal(err)
			}
			got := make([]string, len(found))
			for i, r := range found {
				got[i] = r.Get("name").(types.FullName).FirstName
			}
			if strings.Join(got, ",") != strings.Join(s.want, ",") {
				t.Fatalf("want %v, got %v", s.want, got)
			}
		})
	}

	if _, err := app.FindRecordsByFilter(c, `name.middleName = "x"`, "", 0, 0); err == nil {
		t.Fatal("a path that names no part must be refused")
	}
}

func TestActorFieldIsStampedNotSet(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	c := crmCollection(t, app)

	r := core.NewRecord(c)
	r.Set("createdBy", types.Actor{Source: "manual", Id: "forged", Name: "Mallory"})
	ann := types.Actor{Source: core.ActorManual, Id: "ann", Name: "Ann"}
	if err := app.SaveWithContext(core.WithActor(context.Background(), ann), r); err != nil {
		t.Fatal(err)
	}
	if got := r.Get("createdBy").(types.Actor); got != ann {
		t.Fatalf("createdBy: want %+v, got %+v", ann, got)
	}

	bob := types.Actor{Source: core.ActorImport, Id: "bob", Name: "Bob"}
	r.Set("rating", 3)
	if err := app.SaveWithContext(core.WithActor(context.Background(), bob), r); err != nil {
		t.Fatal(err)
	}

	got, _ := app.FindRecordById(c, r.Id)
	if v := got.Get("createdBy").(types.Actor); v != ann {
		t.Fatalf("createdBy must keep the first writer, got %+v", v)
	}
	if v := got.Get("updatedBy").(types.Actor); v != bob {
		t.Fatalf("updatedBy must be the last writer, got %+v", v)
	}

	// a save that states nobody is the system's
	r2 := core.NewRecord(c)
	if err := app.Save(r2); err != nil {
		t.Fatal(err)
	}
	if v := r2.Get("createdBy").(types.Actor); v.Source != core.ActorSystem {
		t.Fatalf("an unstated writer is the system, got %+v", v)
	}

	// "my records" is a rule over the actor's id
	mine, err := app.FindRecordsByFilter(c, `createdBy.id = "ann"`, "", 0, 0)
	if err != nil || len(mine) != 1 {
		t.Fatalf("filter by actor id: %d records, err %v", len(mine), err)
	}
}

func TestFieldMetaAndOptionsValidation(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	scenarios := []struct {
		name  string
		field core.Field
		code  string
	}{
		{"label too long", &core.TextField{Name: "a", FieldMeta: core.FieldMeta{Label: strings.Repeat("x", 256)}}, "validation_length_too_long"},
		{"icon with a space", &core.TextField{Name: "a", FieldMeta: core.FieldMeta{Icon: "bad icon"}}, "validation_match_invalid"},
		{"negative position", &core.TextField{Name: "a", FieldMeta: core.FieldMeta{Position: -1}}, "validation_min_greater_equal_than_required"},
		{"option for an unknown value", &core.SelectField{Name: "a", Values: []string{"x"}, Options: []core.SelectOption{{Value: "y"}}}, "validation_unknown_option"},
		{"option colour off the palette", &core.SelectField{Name: "a", Values: []string{"x"}, Options: []core.SelectOption{{Value: "x", Color: "chartreuse"}}}, "validation_invalid_color"},
		{"two options for one value", &core.SelectField{Name: "a", Values: []string{"x"}, Options: []core.SelectOption{{Value: "x"}, {Value: "x"}}}, "validation_duplicated_option"},
		{"unknown default currency", &core.CurrencyField{Name: "a", DefaultCurrency: "ABC"}, "validation_currency_code"},
		{"rating with too many levels", &core.RatingField{Name: "a", Max: 11}, "validation_max_less_equal_than_required"},
		{"actor that stamps nothing", &core.ActorField{Name: "a"}, "validation_required"},
	}

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			c := core.NewBaseCollection("meta_" + strings.ReplaceAll(s.name, " ", "_"))
			c.Fields.Add(s.field)
			err := app.Save(c)
			if err == nil {
				t.Fatal("expected the collection to be refused")
			}
			if codes := errorCodes(err); !strings.Contains(codes, s.code) {
				t.Fatalf("expected %q in %s", s.code, codes)
			}
		})
	}
}

func TestCollectionBaseOptions(t *testing.T) {
	app, _ := tests.NewTestApp()
	defer app.Cleanup()

	c := core.NewBaseCollection("people")
	if err := json.Unmarshal([]byte(`{"history":true,"label":"Person","plural":"People","icon":"IconUser","description":"People we know"}`), c); err != nil {
		t.Fatal(err)
	}
	if err := app.Save(c); err != nil {
		t.Fatal(err)
	}

	got, err := app.FindCollectionByNameOrId("people")
	if err != nil {
		t.Fatal(err)
	}
	if !got.History || got.Label != "Person" || got.Plural != "People" || got.Icon != "IconUser" {
		t.Fatalf("options did not survive the save: %s", got)
	}

	raw, _ := json.Marshal(got)
	for _, want := range []string{`"history":true`, `"label":"Person"`, `"plural":"People"`, `"icon":"IconUser"`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("expected %s in %s", want, raw)
		}
	}

	bad := core.NewBaseCollection("bad_icon")
	bad.Icon = "no spaces allowed"
	if err := app.Save(bad); err == nil {
		t.Fatal("expected a bad icon to be refused")
	}
}
