// Package crm is the CRM as a Base module: the collections an org's sales,
// hiring and outreach run on, installed into the org's own Base.
//
//	companies, people, opportunities   the pipeline
//	candidates, applications           hiring and programme intake
//	lists, list_members                hand-picked sets of people
//	segments                           saved filters, read whenever they are used
//	activities                         the timeline: one row per event, once per source id
//	notes, tasks, attachments          what is kept on a record
//	views                              saved views of a collection, the org's and a person's own
//
// A view is the org's (visibility org, or empty) or its maker's (private): a
// private view is read, changed and deleted by the person who made it and by
// nobody else, an org admin included. Which view a person opens first is their
// IAM preference, and the only part of a view kept there.
//
// Every collection records who created and last updated a row, and the
// pipeline, hiring, notes and tasks keep their history. Members of the org read
// and write; a list, a segment or an org view is deleted by whoever made it or by
// an org admin, and an activity only by an org admin.
//
// Blank-import the package to offer the module; install it with
// POST /v1/modules/crm.
package crm

import (
	_ "embed"
	"encoding/json"

	"github.com/hanzoai/base/modules"
)

// Name is the module's name.
const Name = "crm"

//go:embed crm.json
var document []byte

func init() {
	var collections []map[string]any
	if err := json.Unmarshal(document, &collections); err != nil {
		panic("crm: crm.json does not decode: " + err.Error())
	}

	modules.Register(modules.Module{
		Name:        Name,
		Title:       "CRM",
		Description: "People, companies, opportunities, candidates and applications, with lists, segments, the activity timeline, notes, tasks, attachments and saved views.",
		Collections: collections,
	})
}
