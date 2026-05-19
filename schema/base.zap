# Hanzo Base — ZAP Schema
#
# Server: base (Go) at base.hanzo.svc.cluster.local or via the unified
# cloud binary at api.hanzo.ai/v1/base.
#
# This schema is the minimum ZAP-typed public surface needed for the
# HIP-0106 Mount() contract. The full HTTP surface — per-tenant SQLite
# CRUD, settings, backups, files, realtime, batch, collection import,
# the HIP-0105 extension runtime substrate — is registered in apis/
# and remains the source of truth for /v1/base/* routes. Wider ZAP-typed
# handlers (Open, Query, Subscribe, …) will land as separate schema PRs
# alongside the in-process BaseClient interface from
# cloud/pkg/cloud/deps.go.
#
# Code generation:
#   zapc generate schema/base.zap --lang go   --out ./gen/zap/
#   zapc generate schema/base.zap --lang ts   --out ./gen/zap/

# ── Health ────────────────────────────────────────────────────────────────

struct HealthRequest
  # No fields. Probe is a side-effect-free GET.

struct HealthResponse
  status   Text
  service  Text

# ── Service interface ────────────────────────────────────────────────────

interface BaseService
  # Liveness probe. Always answers ok unless the process is unreachable.
  # Mounted at GET /v1/base/health by Mount(app, deps).
  health (request HealthRequest) -> (response HealthResponse)
