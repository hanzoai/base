package apis

import (
	"context"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/types"
)

// WithRequestActor is the request's context with the writer stated, for the
// records the request saves: an actor field stamps it and a collection's
// history records it (see [core.WithActor]).
func WithRequestActor(e *core.RequestEvent) context.Context {
	return core.WithActor(e.Request.Context(), RequestActor(e))
}

// RequestActor answers who a request writes as: the person it authenticated as,
// an org's own key, or — with no credential — an anonymous caller of a public
// route.
func RequestActor(e *core.RequestEvent) types.Actor {
	if e.Auth != nil {
		name := e.Auth.GetString("name")
		if name == "" {
			name = e.Auth.GetString(core.FieldNameEmail)
		}
		return types.Actor{Source: core.ActorManual, Id: e.Auth.Id, Name: name}
	}

	// An IAM key mints no auth record, so its door names the subject instead.
	if sub, _ := e.Get(RequestEventKeySub).(string); sub != "" {
		name, _ := e.Get(RequestEventKeyName).(string)
		return types.Actor{Source: core.ActorAPI, Id: sub, Name: name}
	}

	return types.Actor{Source: core.ActorAPI, Name: "Anonymous"}
}
