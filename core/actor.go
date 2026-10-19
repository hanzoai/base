package core

import (
	"context"

	"github.com/hanzoai/base/tools/types"
)

// Where a write came from. A request a person makes is manual, an org's own key
// is api, and the rest are named by the program that states them.
const (
	ActorManual      = "manual"
	ActorAPI         = "api"
	ActorImport      = "import"
	ActorEmail       = "email"
	ActorCalendar    = "calendar"
	ActorWorkflow    = "workflow"
	ActorAgent       = "agent"
	ActorWebhook     = "webhook"
	ActorApplication = "application"
	ActorSystem      = "system"
)

// ActorSources is every source an actor may name.
var ActorSources = []string{
	ActorManual, ActorAPI, ActorImport, ActorEmail, ActorCalendar,
	ActorWorkflow, ActorAgent, ActorWebhook, ActorApplication, ActorSystem,
}

type actorKey struct{}

// WithActor states who is writing, for every record saved with the returned
// context: an actor field stamps it and a collection's history records it.
func WithActor(ctx context.Context, actor types.Actor) context.Context {
	return context.WithValue(ctx, actorKey{}, actor)
}

// ActorFrom answers who is writing. A write nobody stated is the system's.
func ActorFrom(ctx context.Context) types.Actor {
	if ctx != nil {
		if a, ok := ctx.Value(actorKey{}).(types.Actor); ok && !a.IsZero() {
			return a
		}
	}
	return types.Actor{Source: ActorSystem, Name: "System"}
}
