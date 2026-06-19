// Package models defines the Base collections backing the bootnode plugin.
//
// These are the Go-native equivalents of the Python SQLAlchemy models in
// bootnode/db/models.py. One deliberate difference: there is NO `users`
// collection. Identity is owned by Hanzo IAM — bootnode records reference IAM
// user ids as plain text fields (ownerId, invitedBy, userId). Duplicating users
// in Base would create two sources of truth; IAM is the only one.
//
// All collections are prefixed `_bootnode_` and owned by the plugin (no public
// CRUD). The plugin's HTTP handlers are the only write path; superusers retain
// dashboard access.
//
// Tenancy: when the platform plugin runs with PrincipalIsolation="sqlite",
// these collections are resolved per-org automatically by the platform's
// org-DB middleware. The OrgCluster collection is the canonical
// org→k8s-cluster mapping that scopes fleet operations.
package models

import (
	"database/sql"
	"errors"
	"fmt"

	"github.com/hanzoai/base/core"
	"github.com/hanzoai/base/tools/types"
)

// Collection names. One prefix, one convention.
const (
	Projects          = "_bootnode_projects"
	APIKeys           = "_bootnode_api_keys"
	Webhooks          = "_bootnode_webhooks"
	WebhookDeliveries = "_bootnode_webhook_deliveries"
	Usage             = "_bootnode_usage"
	SmartWallets      = "_bootnode_smart_wallets"
	TeamMembers       = "_bootnode_team_members"
	GasPolicies       = "_bootnode_gas_policies"
	Subscriptions     = "_bootnode_subscriptions"
	OrgClusters       = "_bootnode_org_clusters"
)

// EnsureAll idempotently creates every bootnode collection in dependency
// order (Projects first, since most others relate to it). Safe to call on
// every boot.
func EnsureAll(app core.App) error {
	projects, err := ensureProjects(app)
	if err != nil {
		return err
	}
	if _, err := ensureAPIKeys(app, projects); err != nil {
		return err
	}
	webhooks, err := ensureWebhooks(app, projects)
	if err != nil {
		return err
	}
	if _, err := ensureWebhookDeliveries(app, webhooks); err != nil {
		return err
	}
	if _, err := ensureUsage(app, projects); err != nil {
		return err
	}
	if _, err := ensureSmartWallets(app, projects); err != nil {
		return err
	}
	if _, err := ensureTeamMembers(app, projects); err != nil {
		return err
	}
	if _, err := ensureGasPolicies(app, projects); err != nil {
		return err
	}
	if _, err := ensureSubscriptions(app, projects); err != nil {
		return err
	}
	if _, err := ensureOrgClusters(app, projects); err != nil {
		return err
	}
	return nil
}

// lookup returns an existing collection or (nil, nil) when absent.
func lookup(app core.App, name string) (*core.Collection, error) {
	c, err := app.FindCollectionByNameOrId(name)
	if err == nil {
		return c, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return nil, fmt.Errorf("bootnode/models: lookup %s: %w", name, err)
}

func ensureProjects(app core.App) (*core.Collection, error) {
	if c, err := lookup(app, Projects); err != nil || c != nil {
		return c, err
	}
	c := core.NewBaseCollection(Projects)
	c.System = true
	c.Fields.Add(
		&core.TextField{Name: "name", Required: true, Min: 1, Max: 255, Presentable: true},
		// IAM user id — owner of the project. Not a Base relation (IAM owns users).
		&core.TextField{Name: "ownerId", Required: true, Max: 255},
		// IAM org slug (hanzo, lux, zoo, pars). Scopes the project.
		&core.TextField{Name: "orgId", Required: true, Max: 100},
		&core.TextField{Name: "description", Max: 2000},
		&core.JSONField{Name: "settings"},
		&core.JSONField{Name: "allowedChains"},
		&core.AutodateField{Name: "created", OnCreate: true},
		&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
	)
	c.Indexes = types.JSONArray[string]{
		fmt.Sprintf("CREATE INDEX `idx_%s_owner` ON `%s` (`ownerId`)", Projects, Projects),
		fmt.Sprintf("CREATE INDEX `idx_%s_org` ON `%s` (`orgId`)", Projects, Projects),
	}
	return c, app.Save(c)
}

func ensureAPIKeys(app core.App, projects *core.Collection) (*core.Collection, error) {
	if c, err := lookup(app, APIKeys); err != nil || c != nil {
		return c, err
	}
	c := core.NewBaseCollection(APIKeys)
	c.System = true
	c.Fields.Add(
		&core.RelationField{Name: "project", CollectionId: projects.Id, Required: true, CascadeDelete: true, MaxSelect: 1},
		&core.TextField{Name: "name", Required: true, Max: 255},
		// SHA-256(rawKey + salt). Never the raw key — the raw key is shown once
		// at creation and never persisted.
		&core.TextField{Name: "keyHash", Required: true, Max: 255},
		&core.TextField{Name: "keyPrefix", Required: true, Max: 16, Presentable: true},
		&core.NumberField{Name: "rateLimit"},
		&core.NumberField{Name: "computeUnitsLimit"},
		&core.JSONField{Name: "allowedOrigins"},
		&core.JSONField{Name: "allowedChains"},
		&core.BoolField{Name: "isActive"},
		// Caller-set on each authenticated use — not an auto-date.
		&core.DateField{Name: "lastUsedAt"},
		&core.AutodateField{Name: "created", OnCreate: true},
	)
	c.Indexes = types.JSONArray[string]{
		fmt.Sprintf("CREATE UNIQUE INDEX `idx_%s_hash` ON `%s` (`keyHash`)", APIKeys, APIKeys),
		fmt.Sprintf("CREATE INDEX `idx_%s_project` ON `%s` (`project`)", APIKeys, APIKeys),
	}
	return c, app.Save(c)
}

func ensureWebhooks(app core.App, projects *core.Collection) (*core.Collection, error) {
	if c, err := lookup(app, Webhooks); err != nil || c != nil {
		return c, err
	}
	c := core.NewBaseCollection(Webhooks)
	c.System = true
	c.Fields.Add(
		&core.RelationField{Name: "project", CollectionId: projects.Id, Required: true, CascadeDelete: true, MaxSelect: 1},
		&core.TextField{Name: "name", Required: true, Max: 255},
		&core.URLField{Name: "url", Required: true},
		&core.TextField{Name: "chain", Required: true, Max: 50},
		&core.TextField{Name: "network", Required: true, Max: 50},
		&core.TextField{Name: "eventType", Required: true, Max: 50},
		&core.JSONField{Name: "filters"},
		// HMAC signing secret for delivery authentication.
		&core.TextField{Name: "secret", Required: true, Max: 255},
		&core.BoolField{Name: "isActive"},
		&core.NumberField{Name: "failureCount"},
		&core.DateField{Name: "lastTriggeredAt"},
		&core.AutodateField{Name: "created", OnCreate: true},
	)
	c.Indexes = types.JSONArray[string]{
		fmt.Sprintf("CREATE INDEX `idx_%s_project` ON `%s` (`project`)", Webhooks, Webhooks),
	}
	return c, app.Save(c)
}

func ensureWebhookDeliveries(app core.App, webhooks *core.Collection) (*core.Collection, error) {
	if c, err := lookup(app, WebhookDeliveries); err != nil || c != nil {
		return c, err
	}
	c := core.NewBaseCollection(WebhookDeliveries)
	c.System = true
	c.Fields.Add(
		&core.RelationField{Name: "webhook", CollectionId: webhooks.Id, Required: true, CascadeDelete: true, MaxSelect: 1},
		&core.JSONField{Name: "payload", Required: true},
		&core.NumberField{Name: "statusCode"},
		&core.TextField{Name: "responseBody", Max: 5000},
		&core.NumberField{Name: "attemptCount"},
		&core.BoolField{Name: "success"},
		&core.TextField{Name: "error", Max: 2000},
		&core.AutodateField{Name: "deliveredAt", OnCreate: true},
	)
	c.Indexes = types.JSONArray[string]{
		fmt.Sprintf("CREATE INDEX `idx_%s_webhook` ON `%s` (`webhook`)", WebhookDeliveries, WebhookDeliveries),
	}
	return c, app.Save(c)
}

func ensureUsage(app core.App, projects *core.Collection) (*core.Collection, error) {
	if c, err := lookup(app, Usage); err != nil || c != nil {
		return c, err
	}
	c := core.NewBaseCollection(Usage)
	c.System = true
	c.Fields.Add(
		&core.RelationField{Name: "project", CollectionId: projects.Id, Required: true, CascadeDelete: true, MaxSelect: 1},
		// apiKey is nullable in the Python schema (SET NULL); a non-required
		// relation captures that.
		&core.RelationField{Name: "apiKey", CollectionId: mustAPIKeysID(app), MaxSelect: 1},
		&core.TextField{Name: "chain", Required: true, Max: 50},
		&core.TextField{Name: "network", Required: true, Max: 50},
		&core.TextField{Name: "method", Required: true, Max: 100},
		&core.NumberField{Name: "computeUnits"},
		&core.NumberField{Name: "responseTimeMs"},
		&core.NumberField{Name: "statusCode"},
		&core.AutodateField{Name: "timestamp", OnCreate: true},
	)
	c.Indexes = types.JSONArray[string]{
		fmt.Sprintf("CREATE INDEX `idx_%s_project_ts` ON `%s` (`project`, `timestamp`)", Usage, Usage),
	}
	return c, app.Save(c)
}

func ensureSmartWallets(app core.App, projects *core.Collection) (*core.Collection, error) {
	if c, err := lookup(app, SmartWallets); err != nil || c != nil {
		return c, err
	}
	c := core.NewBaseCollection(SmartWallets)
	c.System = true
	c.Fields.Add(
		&core.RelationField{Name: "project", CollectionId: projects.Id, Required: true, CascadeDelete: true, MaxSelect: 1},
		&core.TextField{Name: "address", Required: true, Max: 42, Presentable: true},
		&core.TextField{Name: "ownerAddress", Required: true, Max: 42},
		&core.TextField{Name: "factoryAddress", Required: true, Max: 42},
		&core.TextField{Name: "chain", Required: true, Max: 50},
		&core.TextField{Name: "network", Required: true, Max: 50},
		&core.TextField{Name: "salt", Required: true, Max: 66},
		&core.BoolField{Name: "isDeployed"},
		&core.JSONField{Name: "walletMetadata"},
		&core.AutodateField{Name: "created", OnCreate: true},
	)
	c.Indexes = types.JSONArray[string]{
		fmt.Sprintf("CREATE UNIQUE INDEX `idx_%s_address` ON `%s` (`address`)", SmartWallets, SmartWallets),
	}
	return c, app.Save(c)
}

func ensureTeamMembers(app core.App, projects *core.Collection) (*core.Collection, error) {
	if c, err := lookup(app, TeamMembers); err != nil || c != nil {
		return c, err
	}
	c := core.NewBaseCollection(TeamMembers)
	c.System = true
	c.Fields.Add(
		&core.RelationField{Name: "project", CollectionId: projects.Id, Required: true, CascadeDelete: true, MaxSelect: 1},
		// IAM user id (nullable until the invited email maps to an IAM user).
		&core.TextField{Name: "userId", Max: 255},
		&core.EmailField{Name: "email", Required: true},
		&core.TextField{Name: "name", Max: 255},
		&core.SelectField{Name: "role", Required: true, MaxSelect: 1, Values: []string{"owner", "admin", "member", "viewer"}},
		&core.SelectField{Name: "status", Required: true, MaxSelect: 1, Values: []string{"pending", "active"}},
		&core.TextField{Name: "inviteToken", Max: 255},
		&core.TextField{Name: "invitedBy", Max: 255},
		&core.DateField{Name: "joinedAt"},
		&core.AutodateField{Name: "created", OnCreate: true},
	)
	c.Indexes = types.JSONArray[string]{
		fmt.Sprintf("CREATE UNIQUE INDEX `idx_%s_project_email` ON `%s` (`project`, `email` COLLATE NOCASE)", TeamMembers, TeamMembers),
	}
	return c, app.Save(c)
}

func ensureGasPolicies(app core.App, projects *core.Collection) (*core.Collection, error) {
	if c, err := lookup(app, GasPolicies); err != nil || c != nil {
		return c, err
	}
	c := core.NewBaseCollection(GasPolicies)
	c.System = true
	c.Fields.Add(
		&core.RelationField{Name: "project", CollectionId: projects.Id, Required: true, CascadeDelete: true, MaxSelect: 1},
		&core.TextField{Name: "name", Required: true, Max: 255},
		&core.TextField{Name: "chain", Required: true, Max: 50},
		&core.TextField{Name: "network", Required: true, Max: 50},
		&core.JSONField{Name: "rules", Required: true},
		&core.NumberField{Name: "maxGasPerOp"},
		&core.NumberField{Name: "maxSpendPerDayUsd"},
		&core.JSONField{Name: "allowedContracts"},
		&core.JSONField{Name: "allowedMethods"},
		&core.BoolField{Name: "isActive"},
		&core.AutodateField{Name: "created", OnCreate: true},
	)
	return c, app.Save(c)
}

func ensureSubscriptions(app core.App, projects *core.Collection) (*core.Collection, error) {
	if c, err := lookup(app, Subscriptions); err != nil || c != nil {
		return c, err
	}
	c := core.NewBaseCollection(Subscriptions)
	c.System = true
	c.Fields.Add(
		&core.RelationField{Name: "project", CollectionId: projects.Id, Required: true, CascadeDelete: true, MaxSelect: 1},
		&core.SelectField{Name: "tier", Required: true, MaxSelect: 1, Values: []string{"free", "starter", "pro", "enterprise"}},
		// Hanzo Commerce linkage (Square backend).
		&core.TextField{Name: "commerceSubscriptionId", Max: 255},
		&core.TextField{Name: "commerceCustomerId", Max: 255},
		&core.NumberField{Name: "monthlyCuLimit"},
		&core.NumberField{Name: "rateLimitPerSecond"},
		&core.NumberField{Name: "maxApps"},
		&core.NumberField{Name: "maxWebhooks"},
		&core.NumberField{Name: "currentCuUsed"},
		&core.DateField{Name: "billingCycleStart"},
		&core.DateField{Name: "billingCycleEnd"},
		&core.TextField{Name: "scheduledTier", Max: 50},
		&core.AutodateField{Name: "created", OnCreate: true},
		&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
	)
	c.Indexes = types.JSONArray[string]{
		fmt.Sprintf("CREATE UNIQUE INDEX `idx_%s_project` ON `%s` (`project`)", Subscriptions, Subscriptions),
	}
	return c, app.Save(c)
}

func ensureOrgClusters(app core.App, projects *core.Collection) (*core.Collection, error) {
	if c, err := lookup(app, OrgClusters); err != nil || c != nil {
		return c, err
	}
	c := core.NewBaseCollection(OrgClusters)
	c.System = true
	c.Fields.Add(
		&core.RelationField{Name: "project", CollectionId: projects.Id, Required: true, CascadeDelete: true, MaxSelect: 1},
		// Canonical org→cluster mapping. clusterId is the DOKS/provider id.
		&core.TextField{Name: "clusterId", Required: true, Max: 255, Presentable: true},
		&core.TextField{Name: "clusterName", Max: 255},
		&core.SelectField{Name: "provider", Required: true, MaxSelect: 1, Values: []string{"digitalocean", "gke", "eks", "aks", "bare"}},
		&core.TextField{Name: "region", Required: true, Max: 50},
		&core.JSONField{Name: "allowedChains"},
		&core.JSONField{Name: "allowedNamespaces"},
		&core.BoolField{Name: "isActive"},
		&core.AutodateField{Name: "created", OnCreate: true},
	)
	c.Indexes = types.JSONArray[string]{
		fmt.Sprintf("CREATE INDEX `idx_%s_project` ON `%s` (`project`)", OrgClusters, OrgClusters),
		fmt.Sprintf("CREATE INDEX `idx_%s_cluster` ON `%s` (`clusterId`)", OrgClusters, OrgClusters),
	}
	return c, app.Save(c)
}

// mustAPIKeysID returns the API keys collection id (it is created before Usage
// in EnsureAll). It returns "" if absent, which yields a relation with no
// target — only reachable if EnsureAll's ordering is violated, so it is a
// programming error rather than a runtime condition.
func mustAPIKeysID(app core.App) string {
	c, err := app.FindCollectionByNameOrId(APIKeys)
	if err != nil {
		return ""
	}
	return c.Id
}
