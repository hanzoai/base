package core

import "testing"

func TestResolveStorageTier(t *testing.T) {
	cases := []struct {
		name     string
		env      map[string]string
		wantTier StorageTier
		wantData string
		wantAux  string
		wantErr  bool
	}{
		{
			name:     "default is sqlite, no DSN",
			env:      nil,
			wantTier: TierSQLite,
		},
		{
			name:     "explicit sqlite",
			env:      map[string]string{"BASE_DB_TIER": "sqlite"},
			wantTier: TierSQLite,
		},
		{
			name:     "case/space insensitive",
			env:      map[string]string{"BASE_DB_TIER": "  SQLite "},
			wantTier: TierSQLite,
		},
		{
			name:     "sql with BASE_DB_URL → data+aux DSN",
			env:      map[string]string{"BASE_DB_TIER": "sql", "BASE_DB_URL": "postgres://u:p@h/db"},
			wantTier: TierSQL,
			wantData: "postgres://u:p@h/db",
			wantAux:  "postgres://u:p@h/db",
		},
		{
			name:     "sql with separate aux DSN",
			env:      map[string]string{"BASE_DB_TIER": "sql", "BASE_DATA_DSN": "postgres://data", "BASE_AUX_DSN": "postgres://aux"},
			wantTier: TierSQL,
			wantData: "postgres://data",
			wantAux:  "postgres://aux",
		},
		{
			name:    "sql without a URL is a loud error",
			env:     map[string]string{"BASE_DB_TIER": "sql"},
			wantErr: true,
		},
		{
			name:    "datastore is reserved (honest error, no silent fallback)",
			env:     map[string]string{"BASE_DB_TIER": "datastore"},
			wantErr: true,
		},
		{
			name:    "unknown tier errors",
			env:     map[string]string{"BASE_DB_TIER": "mongodb"},
			wantErr: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, k := range []string{"BASE_DB_TIER", "BASE_DB_URL", "BASE_DATA_DSN", "BASE_AUX_DSN"} {
				t.Setenv(k, "")
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}

			tier, data, aux, err := ResolveStorageTier()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got tier=%q data=%q", tier, data)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tier != tc.wantTier {
				t.Errorf("tier = %q, want %q", tier, tc.wantTier)
			}
			if data != tc.wantData {
				t.Errorf("dataDSN = %q, want %q", data, tc.wantData)
			}
			if aux != tc.wantAux {
				t.Errorf("auxDSN = %q, want %q", aux, tc.wantAux)
			}
		})
	}
}
