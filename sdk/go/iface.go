// Package base — canonical client interface for Hanzo Base.
//
//	import base "github.com/hanzoai/base/sdk/go"
//	var b base.Client = base.NewClient(cfg)

package base

import (
	"context"
	"time"
)

// Client is the Base surface: typed collection CRUD, realtime
// subscription, file attachment, schema introspection. Always scoped
// to a single Base instance (one URL); multi-instance fan-out is the
// caller's concern.
type Client interface {
	// Kind reports the backend identifier (hanzo-base | pocketbase).
	Kind() string

	// Authenticate exchanges a user identity for a session token. Empty
	// password selects the OAuth2/PKCE flow (the impl handles the
	// redirect-callback dance via the http.Handler returned by
	// AuthHandler).
	Authenticate(ctx context.Context, email, password string) (*Session, error)

	// AuthAs sets the auth context for subsequent calls. Passing an
	// empty token logs out.
	AuthAs(token string)

	// Find returns one record by id. ErrRecordNotFound on miss.
	Find(ctx context.Context, collection, id string) (*Record, error)

	// List returns records matching filter, paginated. Filter syntax
	// follows the Base query DSL: field=value, field~"prefix*",
	// field >= 100, conjunction with && and ||.
	List(ctx context.Context, collection, filter string, opts ListOpts) (*Page, error)

	// Create inserts a record. The id field of `data` is ignored — the
	// backend assigns the id and returns it on the result.
	Create(ctx context.Context, collection string, data map[string]any) (*Record, error)

	// Update mutates a record. Partial — only keys present in `patch`
	// are written.
	Update(ctx context.Context, collection, id string, patch map[string]any) (*Record, error)

	// Delete removes a record. Idempotent.
	Delete(ctx context.Context, collection, id string) error

	// Subscribe opens a realtime stream of changes on a collection. The
	// returned channel emits Events until ctx is cancelled or the
	// connection breaks.
	Subscribe(ctx context.Context, collection, filter string) (<-chan Event, error)

	// Schema returns the field definitions for a collection.
	Schema(ctx context.Context, collection string) (*CollectionSchema, error)

	// UploadFile attaches a file to a record's file field. Returns the
	// internal storage key (used in record.Get(field) to read back).
	UploadFile(ctx context.Context, collection, recordID, field string, name string, body []byte) (string, error)
}

// Session is the result of Authenticate.
type Session struct {
	Token     string
	UserID    string
	Email     string
	ExpiresAt time.Time
}

// Record is the canonical row shape.
type Record struct {
	ID         string
	Collection string
	Created    time.Time
	Updated    time.Time
	// Fields holds the user-defined column values keyed by field name.
	Fields map[string]any
}

// ListOpts configures a list call.
type ListOpts struct {
	Page    int
	PerPage int
	// Sort is field name, optionally prefixed with - for DESC. Comma-
	// separates multi-key sorts.
	Sort string
	// Expand triggers relation expansion for the named foreign-key
	// fields (one round trip; up to 5 levels).
	Expand []string
}

// Page is one slice of a List result.
type Page struct {
	Items       []Record
	Page        int
	PerPage     int
	TotalItems  int
	TotalPages  int
}

// Event is one realtime change notification.
type Event struct {
	Action     string // create | update | delete
	Collection string
	Record     *Record
	// At is the server-side timestamp of the change.
	At time.Time
}

// CollectionSchema describes a collection's field set.
type CollectionSchema struct {
	Name   string
	Type   string // base | auth | view
	System bool
	Fields []FieldSchema
	// Indexes are SQL index definitions registered on the collection.
	Indexes []string
}

// FieldSchema is one field's declared shape.
type FieldSchema struct {
	Name     string
	Type     string // text | number | bool | json | select | relation | file | autodate | ...
	Required bool
	System   bool
	// Options is the type-specific config (select values, relation
	// target id, max file size, ...).
	Options map[string]any
}
