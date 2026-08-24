package router

import "github.com/hanzoai/base/tools/hook"

type Route[T hook.Resolver] struct {
	excludedMiddlewares map[string]struct{}

	// pattern is the whole address the route was mounted at, assembled by
	// [Router.BuildMux] from the method, the group prefixes and the path.
	pattern string

	Action      func(e T) error
	Method      string
	Path        string
	Middlewares []*hook.Handler[T]
}

// Pattern is the whole address the route answers at: its method, every group
// prefix above it and its own path, which is also what the mux reports in
// [net/http.Request.Pattern]. It is empty until the mux is built, because a
// route knows its path but not the groups it was registered under.
//
// A route's own address is otherwise only knowable by reassembling it from the
// group tree, and two assemblies of one address drift.
func (route *Route[T]) Pattern() string {
	return route.pattern
}

// BindFunc registers one or multiple middleware functions to the current route.
//
// The registered middleware functions are "anonymous" and with default priority,
// aka. executes in the order they were registered.
//
// If you need to specify a named middleware (ex. so that it can be removed)
// or middleware with custom exec prirority, use the [Route.Bind] method.
func (route *Route[T]) BindFunc(middlewareFuncs ...func(e T) error) *Route[T] {
	for _, m := range middlewareFuncs {
		route.Middlewares = append(route.Middlewares, &hook.Handler[T]{Func: m})
	}

	return route
}

// Bind registers one or multiple middleware handlers to the current route.
func (route *Route[T]) Bind(middlewares ...*hook.Handler[T]) *Route[T] {
	route.Middlewares = append(route.Middlewares, middlewares...)

	// unmark the newly added middlewares in case they were previously "excluded"
	if route.excludedMiddlewares != nil {
		for _, m := range middlewares {
			if m.Id != "" {
				delete(route.excludedMiddlewares, m.Id)
			}
		}
	}

	return route
}

// Unbind removes one or more middlewares with the specified id(s) from the current route.
//
// It also adds the removed middleware ids to an exclude list so that they could be skipped from
// the execution chain in case the middleware is registered in a parent group.
//
// Anonymous middlewares are considered non-removable, aka. this method
// does nothing if the middleware id is an empty string.
func (route *Route[T]) Unbind(middlewareIds ...string) *Route[T] {
	for _, middlewareId := range middlewareIds {
		if middlewareId == "" {
			continue
		}

		// remove from the route's middlewares
		for i := len(route.Middlewares) - 1; i >= 0; i-- {
			if route.Middlewares[i].Id == middlewareId {
				route.Middlewares = append(route.Middlewares[:i], route.Middlewares[i+1:]...)
			}
		}

		// add to the exclude list
		if route.excludedMiddlewares == nil {
			route.excludedMiddlewares = map[string]struct{}{}
		}
		route.excludedMiddlewares[middlewareId] = struct{}{}
	}

	return route
}
