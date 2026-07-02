package migrations

import (
	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/types"
)

// Connected-accounts + email + calendar sync data model — the foundation for a
// CRM that syncs a user's mailbox and calendar and auto-links them to people/
// companies (the connected-accounts layer).
//
// Persisted as SYSTEM-WRITE-GATED collections (the sync plugins are the only
// writers — CreateRule/UpdateRule/DeleteRule = nil so only a superuser/plugin
// context mutates them), but user-READABLE (owner-scoped List/View), so the
// console renders a user's own channels, messages and events while the sync
// engine owns the writes. System=false so the REST + realtime API expose them.
//
// Split into three concerns (one plugin each, later):
//   connect  — connectedAccount (OAuth tokens, encrypted) + participant matching
//   messaging — messageChannel/message/messageThread/messageParticipant/messageAssociation
//   calendar — calendarChannel/calendarEvent/calendarEventParticipant/calendarEventAssociation
//
// Token fields hold ONLY ciphertext (accessTokenEnc/refreshTokenEnc, AES-256-GCM
// per-user-derived key in dev, Hanzo KMS in prod) — never plaintext, enforced by
// the sync plugin, mirroring the reference's encrypted-envelope model.
func init() {
	core.SystemMigrations.Register(func(txApp core.App) error {
		for _, create := range []func(core.App) error{
			createConnectedAccountCollection,
			createMessageChannelCollection,
			createMessageThreadCollection,
			createMessageCollection,
			createMessageParticipantCollection,
			createMessageAssociationCollection,
			createCalendarChannelCollection,
			createCalendarEventCollection,
			createCalendarEventParticipantCollection,
			createCalendarEventAssociationCollection,
		} {
			if err := create(txApp); err != nil {
				return err
			}
		}
		return nil
	}, func(txApp core.App) error {
		// children before parents.
		for _, name := range []string{
			"calendarEventAssociation", "calendarEventParticipant", "calendarEvent", "calendarChannel",
			"messageAssociation", "messageParticipant", "message", "messageThread", "messageChannel",
			"connectedAccount",
		} {
			if c, err := txApp.FindCollectionByNameOrId(name); err == nil {
				if err := txApp.Delete(c); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// syncCollection builds a plugin-owned collection: the user OWNS its rows (reads
// scoped to owner), but only the sync plugin (superuser context) writes them.
func syncCollection(name string) *core.Collection {
	c := core.NewBaseCollection(name)
	c.System = false
	own := "owner = @request.auth.id"
	c.ListRule = types.Pointer(own)
	c.ViewRule = types.Pointer(own)
	c.CreateRule = nil // plugin/superuser only
	c.UpdateRule = nil
	c.DeleteRule = nil
	c.Fields.Add(&core.TextField{Name: "owner", Required: true}) // IAM user id
	return c
}

// ── connect ───────────────────────────────────────────────────────────────────

func createConnectedAccountCollection(txApp core.App) error {
	c := syncCollection("connectedAccount")
	c.Fields.Add(&core.SelectField{Name: "provider", MaxSelect: 1, Values: []string{"google", "microsoft", "imap_smtp_caldav"}})
	c.Fields.Add(&core.TextField{Name: "handle", Required: true}) // the account email
	c.Fields.Add(&core.JSONField{Name: "handleAliases"})
	c.Fields.Add(&core.JSONField{Name: "scopes"})
	c.Fields.Add(&core.TextField{Name: "accessTokenEnc"})  // ciphertext ONLY
	c.Fields.Add(&core.TextField{Name: "refreshTokenEnc"}) // ciphertext ONLY
	c.Fields.Add(&core.DateField{Name: "authFailedAt"})    // set → surfaces "reconnect"
	c.Fields.Add(&core.DateField{Name: "tokenExpiresAt"})
	addTimestamps(c)
	c.AddIndex("idx_connacct_owner_handle", true, "owner, provider, handle", "")
	return txApp.Save(c)
}

// syncStateFields adds the channel state-machine columns shared by mail + calendar.
func syncStateFields(c *core.Collection) {
	c.Fields.Add(&core.SelectField{Name: "syncStatus", MaxSelect: 1, Values: []string{
		"not_synced", "importing", "synced", "failed_unknown", "failed_insufficient_permissions",
	}})
	c.Fields.Add(&core.SelectField{Name: "syncStage", MaxSelect: 1, Values: []string{
		"pending", "scheduled", "list_fetch", "import", "done",
	}})
	c.Fields.Add(&core.TextField{Name: "syncCursor"}) // historyId / syncToken; empty ⇒ full sync
	c.Fields.Add(&core.DateField{Name: "syncedAt"})
	c.Fields.Add(&core.DateField{Name: "syncStageStartedAt"})
	c.Fields.Add(&core.NumberField{Name: "throttleFailureCount"})
	c.Fields.Add(&core.DateField{Name: "throttleRetryAfter"})
	c.Fields.Add(&core.BoolField{Name: "isSyncEnabled"})
}

// ── messaging ──────────────────────────────────────────────────────────────────

func createMessageChannelCollection(txApp core.App) error {
	c := syncCollection("messageChannel")
	c.Fields.Add(refField("connectedAccount"))
	c.Fields.Add(&core.SelectField{Name: "contactAutoCreationPolicy", MaxSelect: 1, Values: []string{"none", "sent", "sent_and_received"}})
	c.Fields.Add(&core.SelectField{Name: "visibility", MaxSelect: 1, Values: []string{"metadata", "subject", "share_everything"}})
	syncStateFields(c)
	addTimestamps(c)
	c.AddIndex("idx_msgchan_account", false, "connectedAccount", "")
	return txApp.Save(c)
}

func createMessageThreadCollection(txApp core.App) error {
	c := syncCollection("messageThread")
	c.Fields.Add(&core.TextField{Name: "subject"})
	c.Fields.Add(&core.TextField{Name: "threadExternalId"}) // gmail threadId / graph conversationId
	addTimestamps(c)
	c.AddIndex("idx_thread_ext", false, "owner, threadExternalId", "")
	return txApp.Save(c)
}

func createMessageCollection(txApp core.App) error {
	c := syncCollection("message")
	c.Fields.Add(&core.TextField{Name: "headerMessageId", Required: true}) // RFC822 id — dedup key
	c.Fields.Add(&core.TextField{Name: "subject"})
	c.Fields.Add(&core.TextField{Name: "text"}) // HTML→text server-side; render with linkify (no innerHTML)
	c.Fields.Add(&core.DateField{Name: "receivedAt"})
	c.Fields.Add(refField("messageThread"))
	addTimestamps(c)
	c.AddIndex("idx_msg_header", true, "owner, headerMessageId", "")
	return txApp.Save(c)
}

func createMessageParticipantCollection(txApp core.App) error {
	c := syncCollection("messageParticipant")
	c.Fields.Add(&core.TextField{Name: "handle", Required: true}) // lowercased email — the match key
	c.Fields.Add(&core.SelectField{Name: "role", MaxSelect: 1, Values: []string{"from", "to", "cc", "bcc"}})
	c.Fields.Add(&core.TextField{Name: "displayName"})
	c.Fields.Add(refField("message"))
	// Matcher links these later (person/company auto-created + backfilled). The
	// target collections may not exist yet in a fresh Base, so store the resolved
	// ids as plain text refs the plugin fills; a RelationField is added by the
	// CRM-objects migration when person/company exist.
	c.Fields.Add(&core.TextField{Name: "personId"})
	c.Fields.Add(&core.TextField{Name: "userId"})
	addTimestamps(c)
	c.AddIndex("idx_msgpart_handle", false, "owner, handle", "")
	c.AddIndex("idx_msgpart_message", false, "message", "")
	return txApp.Save(c)
}

func createMessageAssociationCollection(txApp core.App) error {
	c := syncCollection("messageAssociation")
	c.Fields.Add(refField("messageChannel"))
	c.Fields.Add(refField("message"))
	c.Fields.Add(&core.TextField{Name: "messageExternalId"})       // provider per-channel id
	c.Fields.Add(&core.TextField{Name: "messageThreadExternalId"}) // provider thread id
	c.Fields.Add(&core.SelectField{Name: "direction", MaxSelect: 1, Values: []string{"incoming", "outgoing"}})
	c.Fields.Add(&core.JSONField{Name: "folderIds"})
	addTimestamps(c)
	c.AddIndex("idx_msgassoc_dedup", true, "message, messageChannel", "")
	return txApp.Save(c)
}

// ── calendar ───────────────────────────────────────────────────────────────────

func createCalendarChannelCollection(txApp core.App) error {
	c := syncCollection("calendarChannel")
	c.Fields.Add(refField("connectedAccount"))
	syncStateFields(c)
	addTimestamps(c)
	c.AddIndex("idx_calchan_account", false, "connectedAccount", "")
	return txApp.Save(c)
}

func createCalendarEventCollection(txApp core.App) error {
	c := syncCollection("calendarEvent")
	c.Fields.Add(&core.TextField{Name: "title"})
	c.Fields.Add(&core.DateField{Name: "startsAt"})
	c.Fields.Add(&core.DateField{Name: "endsAt"})
	c.Fields.Add(&core.BoolField{Name: "isFullDay"})
	c.Fields.Add(&core.BoolField{Name: "isCanceled"})
	c.Fields.Add(&core.TextField{Name: "iCalUid"})
	c.Fields.Add(&core.TextField{Name: "location"})
	c.Fields.Add(&core.TextField{Name: "description"})
	c.Fields.Add(&core.JSONField{Name: "conferenceLink"})
	addTimestamps(c)
	c.AddIndex("idx_calevt_time", false, "owner, startsAt", "")
	return txApp.Save(c)
}

func createCalendarEventParticipantCollection(txApp core.App) error {
	c := syncCollection("calendarEventParticipant")
	c.Fields.Add(&core.TextField{Name: "handle", Required: true})
	c.Fields.Add(&core.SelectField{Name: "responseStatus", MaxSelect: 1, Values: []string{"needs_action", "declined", "tentative", "accepted"}})
	c.Fields.Add(&core.BoolField{Name: "isOrganizer"})
	c.Fields.Add(refField("calendarEvent"))
	c.Fields.Add(&core.TextField{Name: "personId"})
	c.Fields.Add(&core.TextField{Name: "userId"})
	addTimestamps(c)
	c.AddIndex("idx_calpart_handle", false, "owner, handle", "")
	return txApp.Save(c)
}

func createCalendarEventAssociationCollection(txApp core.App) error {
	c := syncCollection("calendarEventAssociation")
	c.Fields.Add(refField("calendarChannel"))
	c.Fields.Add(refField("calendarEvent"))
	c.Fields.Add(&core.TextField{Name: "eventExternalId"})
	c.Fields.Add(&core.TextField{Name: "recurringEventExternalId"}) // links instances to a series
	addTimestamps(c)
	c.AddIndex("idx_calassoc_dedup", true, "calendarEvent, calendarChannel", "")
	return txApp.Save(c)
}

// refField is a forward-compatible text reference to another collection by name.
// Kept as a text id (not a RelationField) so the schema applies before the CRM
// object graph exists; the sync plugin populates it, and a later migration
// upgrades these to real relations once person/company/etc. are present.
func refField(target string) core.Field {
	return &core.TextField{Name: target}
}
