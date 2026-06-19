package bootnode

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"strings"
	"time"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/plugins/bootnode/models"
)

// handleListTeam lists the team members of the caller's project. Ports
// GET /team from bootnode/api/team/__init__.py.
func (p *plugin) handleListTeam(e *core.RequestEvent) error {
	project, err := p.requireProject(e)
	if err != nil {
		return err
	}
	members, err := p.app.FindRecordsByFilter(
		models.TeamMembers,
		"project = {:project}",
		"created", 0, 0,
		map[string]any{"project": project.Id},
	)
	if err != nil {
		return e.InternalServerError("failed to list team members", err)
	}
	out := make([]map[string]any, 0, len(members))
	for _, m := range members {
		out = append(out, memberJSON(m))
	}
	return e.JSON(http.StatusOK, map[string]any{"members": out, "total": len(out)})
}

// handleInviteMember invites a member by email. If the email already maps to an
// IAM user the membership is active immediately; otherwise it is pending with
// an invite token. Ports POST /team.
func (p *plugin) handleInviteMember(e *core.RequestEvent) error {
	id, err := p.requireUser(e)
	if err != nil {
		return err
	}
	if id.ReadOnly {
		return e.ForbiddenError("publishable keys are read-only", nil)
	}
	project, err := p.requireProject(e)
	if err != nil {
		return err
	}

	var body struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("invalid request body", err)
	}
	body.Email = strings.TrimSpace(strings.ToLower(body.Email))
	if body.Email == "" || !strings.Contains(body.Email, "@") {
		return e.BadRequestError("a valid email is required", nil)
	}
	role := body.Role
	if role == "" {
		role = "viewer"
	}
	if !validMemberRole(role) {
		return e.BadRequestError("role must be one of: owner, admin, member, viewer", nil)
	}

	// Reject duplicates within the project.
	existing, _ := p.app.FindRecordsByFilter(
		models.TeamMembers,
		"project = {:project} && email = {:email}",
		"", 1, 0,
		map[string]any{"project": project.Id, "email": body.Email},
	)
	if len(existing) > 0 {
		return e.BadRequestError("member with this email already exists", nil)
	}

	// Does the email already resolve to an IAM user in this org?
	var iamUserID, iamName string
	if users, lookupErr := p.iam.LookupByAttribute(e.Request.Context(), "email", body.Email, id.Org, 1); lookupErr == nil && len(users) > 0 {
		iamUserID = users[0].ID
		iamName = users[0].Name
	}

	col, err := p.app.FindCollectionByNameOrId(models.TeamMembers)
	if err != nil {
		return e.InternalServerError("team members collection not found", err)
	}
	rec := core.NewRecord(col)
	rec.Set("project", project.Id)
	rec.Set("email", body.Email)
	rec.Set("role", role)
	rec.Set("invitedBy", id.UserID)
	if iamUserID != "" {
		rec.Set("userId", iamUserID)
		rec.Set("name", iamName)
		rec.Set("status", "active")
		rec.Set("joinedAt", time.Now().UTC())
	} else {
		rec.Set("status", "pending")
		rec.Set("inviteToken", randomToken())
	}
	if err := p.app.Save(rec); err != nil {
		return e.InternalServerError("failed to create member", err)
	}
	return e.JSON(http.StatusCreated, memberJSON(rec))
}

// handleUpdateMember updates a member's role or display name. Ports
// PATCH /team/{id}.
func (p *plugin) handleUpdateMember(e *core.RequestEvent) error {
	id, err := p.requireUser(e)
	if err != nil {
		return err
	}
	if id.ReadOnly {
		return e.ForbiddenError("publishable keys are read-only", nil)
	}
	project, err := p.requireProject(e)
	if err != nil {
		return err
	}
	member, err := p.memberInProject(project, e.Request.PathValue("id"))
	if err != nil {
		return e.NotFoundError("team member not found", err)
	}

	var body struct {
		Role string `json:"role"`
		Name string `json:"name"`
	}
	if err := e.BindBody(&body); err != nil {
		return e.BadRequestError("invalid request body", err)
	}
	if body.Role != "" {
		if !validMemberRole(body.Role) {
			return e.BadRequestError("invalid role", nil)
		}
		member.Set("role", body.Role)
	}
	if body.Name != "" {
		member.Set("name", body.Name)
	}
	if err := p.app.Save(member); err != nil {
		return e.InternalServerError("failed to update member", err)
	}
	return e.JSON(http.StatusOK, memberJSON(member))
}

// handleRemoveMember removes a member. Ports DELETE /team/{id}.
func (p *plugin) handleRemoveMember(e *core.RequestEvent) error {
	id, err := p.requireUser(e)
	if err != nil {
		return err
	}
	if id.ReadOnly {
		return e.ForbiddenError("publishable keys are read-only", nil)
	}
	project, err := p.requireProject(e)
	if err != nil {
		return err
	}
	member, err := p.memberInProject(project, e.Request.PathValue("id"))
	if err != nil {
		return e.NotFoundError("team member not found", err)
	}
	if err := p.app.Delete(member); err != nil {
		return e.InternalServerError("failed to remove member", err)
	}
	return e.NoContent(http.StatusNoContent)
}

// memberInProject finds a member record and verifies it belongs to project.
func (p *plugin) memberInProject(project *core.Record, memberID string) (*core.Record, error) {
	rec, err := p.app.FindRecordById(models.TeamMembers, memberID)
	if err != nil {
		return nil, err
	}
	if rec.GetString("project") != project.Id {
		return nil, errString("member does not belong to project")
	}
	return rec, nil
}

func memberJSON(m *core.Record) map[string]any {
	return map[string]any{
		"id":       m.Id,
		"email":    m.GetString("email"),
		"name":     m.GetString("name"),
		"role":     m.GetString("role"),
		"status":   m.GetString("status"),
		"joinedAt": m.GetString("joinedAt"),
		"created":  m.GetString("created"),
	}
}

func validMemberRole(role string) bool {
	switch role {
	case "owner", "admin", "member", "viewer":
		return true
	default:
		return false
	}
}

// randomToken returns a URL-safe 32-byte invite token.
func randomToken() string {
	buf := make([]byte, 32)
	_, _ = rand.Read(buf)
	return base64.RawURLEncoding.EncodeToString(buf)
}
