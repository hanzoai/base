package platform

import (
	"log/slog"

	"github.com/hanzoai/base/core"
)

const (
	collectionOrgConfigs   = "_org_configs"
	collectionOrgCustomers = "_org_customers"
)

func (p *plugin) ensureOrgConfigsCollection() error {
	_, err := p.app.FindCollectionByNameOrId(collectionOrgConfigs)
	if err == nil {
		return nil
	}

	c := core.NewBaseCollection(collectionOrgConfigs)
	c.System = true
	c.Fields.Add(
		&core.TextField{Name: "org_id", Required: true, Min: 1, Max: 100},
		&core.TextField{Name: "display_name", Max: 200},
		&core.SelectField{
			Name:      "status",
			MaxSelect: 1,
			Values:    []string{"active", "suspended", "onboarding"},
		},
		&core.TextField{Name: "kms_project_id"},
		&core.JSONField{Name: "fee_schedule", MaxSize: 1 << 20},
		&core.JSONField{Name: "features", MaxSize: 1 << 20},
		&core.JSONField{Name: "providers", MaxSize: 1 << 20},
		&core.JSONField{Name: "chain_config", MaxSize: 1 << 20},
		&core.JSONField{Name: "metadata", MaxSize: 1 << 20},
		&core.AutodateField{Name: "created", OnCreate: true},
		&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
	)

	// Unique index on org_id.
	c.AddIndex("idx_org_configs_org_id", true, "org_id", "")

	p.app.Logger().Info("creating platform system collection", slog.String("name", collectionOrgConfigs))
	return p.app.Save(c)
}

func (p *plugin) ensureOrgCustomersCollection() error {
	_, err := p.app.FindCollectionByNameOrId(collectionOrgCustomers)
	if err == nil {
		return nil
	}

	c := core.NewBaseCollection(collectionOrgCustomers)
	c.System = true
	c.Fields.Add(
		&core.TextField{Name: "org_id", Required: true, Min: 1, Max: 100},
		&core.TextField{Name: "user_id", Required: true, Min: 1, Max: 100},
		&core.TextField{Name: "customer_id", Required: true, Min: 1, Max: 20},
		&core.SelectField{
			Name:      "status",
			MaxSelect: 1,
			Values:    []string{"active", "suspended", "pending_kyc", "closed"},
		},
		&core.TextField{Name: "display_name", Max: 200},
		&core.TextField{Name: "broker_account_id"},
		&core.TextField{Name: "commerce_customer_id"},
		&core.TextField{Name: "mpc_vault_id"},
		&core.JSONField{Name: "metadata", MaxSize: 1 << 20},
		&core.AutodateField{Name: "created", OnCreate: true},
		&core.AutodateField{Name: "updated", OnCreate: true, OnUpdate: true},
	)

	// Unique composite index on (org_id, user_id).
	c.AddIndex("idx_org_customers_org_user", true, "org_id, user_id", "")

	// Unique composite index on (org_id, customer_id).
	c.AddIndex("idx_org_customers_org_custid", true, "org_id, customer_id", "")

	p.app.Logger().Info("creating platform system collection", slog.String("name", collectionOrgCustomers))
	return p.app.Save(c)
}
