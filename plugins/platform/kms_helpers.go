// Environment knobs for the platform-side KMS bridge.

package platform

import (
	"os"
	"strings"
)

// envOr reads an env var, falling back when unset or blank.
func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// nodeIDFromEnv reads $KMS_NODE_ID, defaulting to fallback. This is the ZAP
// transport label (mDNS / peer-table entry), NOT the auth identity — the auth
// identity is mnemonic-derived and lives under servicePathFromEnv.
func nodeIDFromEnv(fallback string) string { return envOr("KMS_NODE_ID", fallback) }

// servicePathFromEnv reads $KMS_SERVICE_PATH, defaulting to fallback. The
// mnemonic-derived identity is built under this path; same path across pods →
// same NodeID byte-for-byte. The kms-operator's consensus-authority snapshot
// enumerates the NodeIDs for every service path.
func servicePathFromEnv(fallback string) string { return envOr("KMS_SERVICE_PATH", fallback) }
