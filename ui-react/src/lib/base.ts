// Base client — thin wrapper around Hanzo Base SDK bound to the host that
// served the page, so the same bundle works against any Base deploy.
//
// The SDK handles auth cookies, realtime EventSource subscriptions, and
// pagination. We expose a single shared instance via `base`.
import { BaseClient } from "/base";

function resolveBaseUrl(): string {
  if (typeof window === 'undefined') return '';
  // Admin UI is served from the same origin as the API, so relative URL works.
  return window.location.origin;
}

export const base = new BaseClient(resolveBaseUrl());

// Export commonly-used handles so pages don't have to reach into the SDK.
export const superusers = base.collection('_superusers');
export const settings = base.settings;
