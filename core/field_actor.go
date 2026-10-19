package core

import (
	"context"

	validation "github.com/go-ozzo/ozzo-validation/v4"
	"github.com/hanzoai/base/core/validators"
	"github.com/hanzoai/base/tools/types"
)

func init() {
	Fields[FieldTypeActor] = func() Field {
		return &ActorField{}
	}
}

const FieldTypeActor = "actor"

var (
	_ Field             = (*ActorField)(nil)
	_ CompositeField    = (*ActorField)(nil)
	_ TextParts         = (*ActorField)(nil)
	_ SetterFinder      = (*ActorField)(nil)
	_ RecordInterceptor = (*ActorField)(nil)
)

// ActorField defines "actor" type field for recording who wrote a record, as
// [types.Actor]: where the write came from, the writer's auth record id and the
// name a person reads.
//
// The server stamps it and no request can set it: OnCreate stamps the writer of
// the record's first save, OnUpdate the writer of each save after it (a field
// with both is "last written by"). The writer is the one the save's context
// states ([WithActor]), the person a request authenticated as, or the system.
//
// `createdBy.id = @request.auth.id` is the rule for "my records".
type ActorField struct {
	// Name (required) is the unique name of the field.
	Name string `form:"name" json:"name"`

	// Id is the unique stable field identifier.
	//
	// It is automatically generated from the name when adding to a collection FieldsList.
	Id string `form:"id" json:"id"`

	// System prevents the renaming and removal of the field.
	System bool `form:"system" json:"system"`

	// Hidden hides the field from the API response.
	Hidden bool `form:"hidden" json:"hidden"`

	// Presentable hints the Dashboard UI to use the underlying
	// field record value in the relation preview label.
	Presentable bool `form:"presentable" json:"presentable"`

	// FieldMeta is how a person sees the field: label, description, icon
	// and position. It changes nothing about the value.
	FieldMeta

	// ---

	// OnCreate stamps the writer of the record's first save.
	OnCreate bool `form:"onCreate" json:"onCreate"`

	// OnUpdate stamps the writer of every later save.
	OnUpdate bool `form:"onUpdate" json:"onUpdate"`
}

// Type implements [Field.Type] interface method.
func (f *ActorField) Type() string {
	return FieldTypeActor
}

// Parts implements [CompositeField.Parts] interface method.
func (f *ActorField) Parts() []string {
	return []string{"source", "id", "name"}
}

// TextParts implements [TextParts.TextParts] interface method.
func (f *ActorField) TextParts() []string {
	return []string{"name", "source"}
}

// GetId implements [Field.GetId] interface method.
func (f *ActorField) GetId() string {
	return f.Id
}

// SetId implements [Field.SetId] interface method.
func (f *ActorField) SetId(id string) {
	f.Id = id
}

// GetName implements [Field.GetName] interface method.
func (f *ActorField) GetName() string {
	return f.Name
}

// SetName implements [Field.SetName] interface method.
func (f *ActorField) SetName(name string) {
	f.Name = name
}

// GetSystem implements [Field.GetSystem] interface method.
func (f *ActorField) GetSystem() bool {
	return f.System
}

// SetSystem implements [Field.SetSystem] interface method.
func (f *ActorField) SetSystem(system bool) {
	f.System = system
}

// GetHidden implements [Field.GetHidden] interface method.
func (f *ActorField) GetHidden() bool {
	return f.Hidden
}

// SetHidden implements [Field.SetHidden] interface method.
func (f *ActorField) SetHidden(hidden bool) {
	f.Hidden = hidden
}

// ColumnType implements [Field.ColumnType] interface method.
func (f *ActorField) ColumnType(app App) string {
	return compositeColumnType(app)
}

// PrepareValue implements [Field.PrepareValue] interface method.
func (f *ActorField) PrepareValue(record *Record, raw any) (any, error) {
	v := types.Actor{}
	err := v.Scan(raw)
	return v, err
}

// ValidateValue implements [Field.ValidateValue] interface method.
func (f *ActorField) ValidateValue(ctx context.Context, app App, record *Record) error {
	if _, ok := record.GetRaw(f.Name).(types.Actor); !ok {
		return validators.ErrUnsupportedValueType
	}
	return nil
}

// ValidateSettings implements [Field.ValidateSettings] interface method.
func (f *ActorField) ValidateSettings(ctx context.Context, app App, collection *Collection) error {
	return validation.ValidateStruct(f,
		validation.Field(&f.Id, validation.By(DefaultFieldIdValidationRule)),
		validation.Field(&f.Name, validation.By(DefaultFieldNameValidationRule)),
		validation.Field(
			&f.OnCreate,
			validation.Required.Error("either onCreate or onUpdate must be enabled").When(!f.OnUpdate),
		),
		validation.Field(
			&f.OnUpdate,
			validation.Required.Error("either onCreate or onUpdate must be enabled").When(!f.OnCreate),
		),
	)
}

// FindSetter implements the [SetterFinder] interface method.
//
// Nothing a caller sends sets the field: the server stamps it.
func (f *ActorField) FindSetter(key string) SetterFunc {
	switch key {
	case f.Name:
		return noopSetter
	default:
		return nil
	}
}

// Intercept implements the [RecordInterceptor] interface.
func (f *ActorField) Intercept(
	ctx context.Context,
	app App,
	record *Record,
	actionName string,
	actionFunc func() error,
) error {
	switch actionName {
	case InterceptorActionCreateExecute:
		if f.OnCreate {
			record.SetRaw(f.Name, ActorFrom(ctx))
		}
	case InterceptorActionUpdateExecute:
		if f.OnUpdate {
			record.SetRaw(f.Name, ActorFrom(ctx))
		}
	}

	return actionFunc()
}
