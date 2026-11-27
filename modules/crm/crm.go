// Package crm is the CRM as a Base module: the collections an org's sales,
// hiring and outreach run on, installed into the org's own Base.
//
//	companies, people, opportunities   the pipelines: sales and fundraising
//	candidates, applications           hiring and programme intake
//	lists, list_members                hand-picked sets of people
//	segments                           saved filters, read whenever they are used
//	activities                         the timeline: one row per event, once per source id
//	notes, tasks, attachments          what is kept on a record
//	views                              saved views of a collection, the org's and a person's own
//	crm_workflows                      record-triggered automations
//	crm_favorites                      records and views pinned by a person
//
// An opportunity is in one pipeline, sales or fundraising, and its stage is one of
// that pipeline's: new, screening, meeting, proposal, customer, lost for a sale;
// intro, meeting, diligence, term_sheet, closed, lost for a round. A person and a
// company have a type (prospect, customer, investor, partner, vendor); a person
// carries where outreach to them stands and when they were last contacted, and an
// opportunity its next step and its last contact, so a table filters and sorts by
// them without reading the timeline.
//
// A view is the org's (visibility org, or empty) or its maker's (private): a
// private view is read, changed and deleted by the person who made it and by
// nobody else, an org admin included. Which view a person opens first is their
// IAM preference, and the only part of a view kept there.
//
// Every collection records who created and last updated a row, and the
// pipeline, hiring, notes and tasks keep their history. Members of the org read
// and write; a list, a segment, a workflow or an org view is deleted by whoever
// made it or by an org admin, a favorite only by its owner, and an activity
// only by an org admin.
//
// Blank-import the package to offer the module; install it with
// POST /v1/modules/crm.
package crm

import (
	_ "embed"
	"encoding/json"
	"fmt"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/modules"
)

// Name is the module's name.
const Name = "crm"

//go:embed crm.json
var document []byte

// StarterView describes a default org view that an application may seed into
// the CRM's views collection after installing the module.
type StarterView struct {
	Name           string         `json:"name"`
	Object         string         `json:"object"`
	Type           string         `json:"type"`
	Icon           string         `json:"icon"`
	Position       int            `json:"position"`
	GroupBy        string         `json:"group_by,omitempty"`
	CalendarField  string         `json:"calendar_field,omitempty"`
	CalendarLayout string         `json:"calendar_layout,omitempty"`
	OpenIn         string         `json:"open_in,omitempty"`
	Where          map[string]any `json:"where,omitempty"`
	IsDefault      bool           `json:"is_default,omitempty"`
}

// StarterViews are the default table, kanban board, list and calendar views for
// a freshly provisioned CRM workspace.
var StarterViews = []StarterView{
	{Name: "All People", Object: "people", Type: "table", Icon: "IconUser", Position: 1, OpenIn: "side", IsDefault: true},
	{Name: "All Companies", Object: "companies", Type: "table", Icon: "IconBuildingSkyscraper", Position: 1, OpenIn: "side", IsDefault: true},
	{Name: "Sales Pipeline", Object: "opportunities", Type: "kanban", Icon: "IconLayoutKanban", Position: 1, GroupBy: "stage", OpenIn: "side", Where: map[string]any{"field": "pipeline", "op": "is", "value": []any{"sales"}}, IsDefault: true},
	{Name: "Fundraising Pipeline", Object: "opportunities", Type: "kanban", Icon: "IconRoute", Position: 2, GroupBy: "stage", OpenIn: "side", Where: map[string]any{"field": "pipeline", "op": "is", "value": []any{"fundraising"}}},
	{Name: "All Opportunities", Object: "opportunities", Type: "table", Icon: "IconTargetArrow", Position: 3, OpenIn: "side"},
	{Name: "Closing Calendar", Object: "opportunities", Type: "calendar", Icon: "IconCalendarEvent", Position: 4, CalendarField: "close_date", CalendarLayout: "month", OpenIn: "side"},
	{Name: "Tasks Board", Object: "tasks", Type: "kanban", Icon: "IconLayoutKanban", Position: 1, GroupBy: "status", OpenIn: "side", IsDefault: true},
	{Name: "Tasks Calendar", Object: "tasks", Type: "calendar", Icon: "IconCalendarDue", Position: 2, CalendarField: "due", CalendarLayout: "month", OpenIn: "side"},
	{Name: "All Tasks", Object: "tasks", Type: "table", Icon: "IconCheckbox", Position: 3, OpenIn: "side"},
	{Name: "All Notes", Object: "notes", Type: "table", Icon: "IconNotes", Position: 1, OpenIn: "side", IsDefault: true},
	{Name: "Activity Timeline", Object: "activities", Type: "list", Icon: "IconTimelineEvent", Position: 1, OpenIn: "side", IsDefault: true},
	{Name: "Activity Calendar", Object: "activities", Type: "calendar", Icon: "IconCalendar", Position: 2, CalendarField: "at", CalendarLayout: "week", OpenIn: "side"},
	{Name: "Candidate Pipeline", Object: "candidates", Type: "kanban", Icon: "IconId", Position: 1, GroupBy: "stage", OpenIn: "side", IsDefault: true},
}

// SeedStarterViews idempotently inserts any missing StarterViews into the views
// collection and returns the number of views created.
func SeedStarterViews(app core.App) (int, error) {
	views, err := app.FindCollectionByNameOrId("views")
	if err != nil {
		return 0, fmt.Errorf("crm: find views collection: %w", err)
	}

	created := 0
	for _, sv := range StarterViews {
		existing, err := app.FindRecordsByFilter(views,
			"object = {:object} && name = {:name} && visibility != 'private'", "", 1, 0,
			map[string]any{"object": sv.Object, "name": sv.Name},
		)
		if err != nil {
			return created, fmt.Errorf("crm: check starter view %q: %w", sv.Name, err)
		}
		if len(existing) > 0 {
			continue
		}

		r := core.NewRecord(views)
		data := map[string]any{
			"name":            sv.Name,
			"object":          sv.Object,
			"type":            sv.Type,
			"icon":            sv.Icon,
			"position":        sv.Position,
			"group_by":        sv.GroupBy,
			"calendar_field":  sv.CalendarField,
			"calendar_layout": sv.CalendarLayout,
			"open_in":         sv.OpenIn,
			"is_default":      sv.IsDefault,
			"visibility":      "org",
		}
		if sv.Where != nil {
			data["where"] = sv.Where
		}
		r.Load(data)
		if err := app.Save(r); err != nil {
			return created, fmt.Errorf("crm: seed starter view %q: %w", sv.Name, err)
		}
		created++
	}

	return created, nil
}

func init() {
	var collections []map[string]any
	if err := json.Unmarshal(document, &collections); err != nil {
		panic("crm: crm.json does not decode: " + err.Error())
	}

	modules.Register(modules.Module{
		Name:        Name,
		Title:       "CRM",
		Description: "People, companies, sales and fundraising opportunities, candidates and applications, with lists, segments, the activity timeline, notes, tasks, attachments, saved views, workflows and favorites.",
		Collections: collections,
	})
}
