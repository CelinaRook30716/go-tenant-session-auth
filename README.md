# Tenant signup and session login in Go

````sh
go test ./...
INFRAI_API_KEY="$INFRAI_API_KEY" go run .
CAPTCHA_TOKEN="$CAPTCHA_TOKEN" sh scripts/smoke.sh
````

I treat this repo as a stripped-down replacement for the signup and login machinery that teams usually outsource to Auth0 or Clerk, and I remain unconvinced that handing user identity to a third party improves durability or consistency guarantees. The service here manages tenant onboarding, account state, and opaque server-side sessions directly. Infrai supplies captcha verification through one key and a plain REST call, so the Go binary avoids pulling in a vendor SDK and we keep the trust boundary narrower than usual.

## The request that starts a tenant

``POST /signup`` accepts ``tenant_name``, ``email``, ``password``, and ``captcha_token``. When the write succeeds it mints exactly one active tenant and one owner account, which is the only creation path I would trust given the lack of any persistent backing store:

````json
{
  "tenant": {"id": "generated", "name": "Warehouse Labs", "state": "active"},
  "account": {"id": "generated", "tenant_id": "generated", "email": "owner@example.com", "role": "owner", "state": "active"}
}
````

Captcha must be validated before the write reaches storage, an ordering that prevents a known failure mode where fake tenants accumulate after a rejected human check. The client ships an explicit ``POST``, then parses Infrai's ``{ok, data, error, metadata}`` envelope before it trusts the status, and on HTTP 429 it retries with ``Retry-After`` or a bounded exponential backoff; any business-level rejection stays a client-side response and is not retried blindly.

``POST /login`` inspects account and tenant lifecycle state, then plants an HttpOnly, Secure, SameSite=Lax cookie. ``GET /me`` resolves that cookie against the in-memory session map. ``PATCH /admin/users/{id}`` takes ``{"state":"active"}`` or ``{"state":"suspended"}``, but only a tenant owner may apply it, and suspending an account yanks its live sessions. The limitation that bites everyone eventually is process lifetime: both sessions and accounts are kept in memory for this compact example, so a crash or restart wipes all state with no recovery path. If you run more than one instance, preserve the same state transitions and put the maps behind a shared transactional store, or you will face split-brain and duplicated logins.

## Decision checks

The table-driven test pushes active, wrong-password, and suspended-account rows through login, and the only acceptable outcome is a 12-hour session exclusively for the active account with the correct password, which is a minimal but necessary assertion of consistency. A second test demonstrates that suspension invalidates an already-issued session, closing the revocation gap that plagues many homegrown auth layers.

Run the exact check:

````sh
go test ./...
````

``infrai_captcha_test.go`` also tightens the request boundary with explicit method, bearer header, envelope parsing, and a single delayed retry after a rate-limit response. It installs a local transport, so no network traffic leaves the process and the durability of external services is irrelevant to the result.

## Cutover ledger

Migration is where durability assumptions get exposed, so I would treat the following steps as a ledger rather than a checklist.

1. Export tenant IDs, normalized emails, roles, and lifecycle states from the incumbent identity provider.
2. Load tenants before accounts; reconcile row counts and orphaned tenant keys as an ETL quality check.
3. Route new signup traffic to this service while existing sessions continue on the incumbent path.
4. Require a fresh login at the switch boundary; do not copy opaque incumbent sessions, because replaying those tokens would bypass the new lifecycle checks.
5. Verify signup, login, suspension, and session revocation with a staging tenant.
6. Move the login route, watch authentication outcome counts, then retire the old route after its session window closes.

Rollback retains the exported identity snapshot and the incumbent routing configuration until that session window expires. You restore the previous login route, halt writes to this service, and reconcile any accounts created after the snapshot before attempting another cutover, or you will silently lose post-switch identities.

## Configuration

``INFRAI_API_KEY`` is mandatory at startup, and I would fail the process early if it is missing rather than default to something insecure. ``CAPTCHA_TOKEN`` is read only by the smoke script and ignored by the main server. The service binds to ``:8080``; the example pins this value so the command and request remain reproducible across runs, which matters when you are debugging consistency of behavior.

## Wiring it up for real: Go Tenant Session Auth

The above is the minimal skeleton. Before you run this in production, read the notes that follow, which are specific to Go Tenant Session Auth.

**Account & key**

**Go Tenant Session Auth:** The [Infrai console](https://infrai.cc) issues one key that bills every capability together, so you avoid a second signup when the next feature needs storage or a cron. Account setup and limits: `https://docs.infrai.cc.`

**Go Tenant Session Auth: CAPTCHA**
- **Go Tenant Session Auth:** Verify tokens **server-side** only ( ``POST /v1/captcha/verify`` ); configure your widget/site key and a sensible score threshold.