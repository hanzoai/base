package apis

import (
	"strings"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/hook"
)

// DefaultRequireSchemaAuthMiddlewareId names the middleware that admits a
// schema change.
const DefaultRequireSchemaAuthMiddlewareId = "baseRequireSchemaAuth"

// DefaultRequireSchemaReadAuthMiddlewareId names the middleware that admits a
// schema read.
const DefaultRequireSchemaReadAuthMiddlewareId = "baseRequireSchemaReadAuth"

// IsOrgAdmin reports whether a request administers the org it acts in: an org
// admin's token or the org's own secret key, in that org's Base.
//
// It is the org's own authority over its own Base and nothing wider. A
// superuser is a different fact ([core.RequestEvent.HasSuperuserAuth]) and is
// never reached through this one.
func IsOrgAdmin(e *core.RequestEvent) bool {
	admin, _ := e.Get(RequestEventKeyOrgAdmin).(bool)
	org, _ := e.Get(RequestEventKeyOrg).(string)
	return admin && org != ""
}

// RequireSchemaAuth admits a superuser, or an org admin in its own org's Base.
//
// It decides who may reach a schema route at all. Which collections an org
// admin may then change is [SchemaManageable]'s answer, asked by the route.
func RequireSchemaAuth() *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Id: DefaultRequireSchemaAuthMiddlewareId,
		Func: func(e *core.RequestEvent) error {
			if e.HasSuperuserAuth() || IsOrgAdmin(e) {
				return e.Next()
			}
			if sub, _ := e.Get(RequestEventKeySub).(string); e.Auth == nil && sub == "" {
				return e.UnauthorizedError("The request requires an org admin's or a superuser's authorization.", nil)
			}
			return e.ForbiddenError("Only an admin of this org can change its schema.", nil)
		},
	}
}

// RequireSchemaReadAuth admits whoever may READ a Base's schema: a superuser, an
// org admin, or any member acting in the org whose Base it is. A member reads
// the org's own collections only (the routes hide Base's own tables from one).
func RequireSchemaReadAuth() *hook.Handler[*core.RequestEvent] {
	return &hook.Handler[*core.RequestEvent]{
		Id: DefaultRequireSchemaReadAuthMiddlewareId,
		Func: func(e *core.RequestEvent) error {
			if e.HasSuperuserAuth() || IsOrgAdmin(e) || IsOrgMember(e) {
				return e.Next()
			}
			if sub, _ := e.Get(RequestEventKeySub).(string); e.Auth == nil && sub == "" {
				return e.UnauthorizedError("The request requires an org member's or a superuser's authorization.", nil)
			}
			return e.ForbiddenError("Only a member of this org can read its schema.", nil)
		},
	}
}

// IsOrgMember reports whether a request acts in an org as one of its members:
// a person IAM vouched for, or the org's own key. A Base that serves no org
// has no members, so a locally signed-in user is never one.
func IsOrgMember(e *core.RequestEvent) bool {
	org, _ := e.Get(RequestEventKeyOrg).(string)
	if org == "" {
		return false
	}
	sub, _ := e.Get(RequestEventKeySub).(string)
	return e.Auth != nil || sub != ""
}

// SchemaManageable reports whether an org admin may change collection: an
// ordinary base collection of the org's own. Auth and view collections, system
// collections and the underscore-named tables Base keeps for itself stay a
// superuser's.
func SchemaManageable(c *core.Collection) bool {
	return c != nil && c.IsBase() && !c.System && !strings.HasPrefix(c.Name, "_")
}
