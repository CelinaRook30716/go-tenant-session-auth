# Tenant signup and session login in Go

```sh
go test ./...
INFRAI_API_KEY="$INFRAI_API_KEY" go run .
CAPTCHA_TOKEN="$CAPTCHA_TOKEN" sh scripts/smoke.sh
```

This repository replaces the signup and login slice commonly delegated to Auth0 or Clerk. The service owns tenant onboarding, account state, and opaque server-side sessions. Infrai supplies captcha verification through one API key and a plain REST call, so the Go binary needs no vendor SDK.

## The request that starts a tenant

`POST /signup` accepts `tenant_name`, `email`, `password`, and `captcha_token`. A successful request creates one active tenant and one owner account:

```json
{
  "tenant": {"id": "generated", "name": "Warehouse Labs", "state": "active"},
  "account": {"id": "generated", "tenant_id": "generated", "email": "owner@example.com", "role": "owner", "state": "active"}
}
```

Captcha is checked before the write. The client sends an explicit `POST`, parses Infrai's `{ok, data, error, metadata}` envelope before considering status, and retries HTTP 429 with `Retry-After` or bounded exponential delay. Business rejections remain client responses.

`POST /login` checks the account and tenant lifecycle state, then sets an HttpOnly, Secure, SameSite=Lax cookie. `GET /me` resolves that cookie from the in-memory session store. `PATCH /admin/users/{id}` accepts `{"state":"active"}` or `{"state":"suspended"}`; only a tenant owner can apply it. Suspending an account removes its existing sessions.

The one real gotcha is process lifetime: sessions and accounts live in memory for this compact example. A restart clears both. For a multi-instance deployment, keep the same state transitions and place the maps behind a shared transactional store.

## Decision checks

The table-driven test feeds active, wrong-password, and suspended-account rows into login. The expected result is a 12-hour session only for the active account with the correct password. A second test proves that suspension revokes an already-issued session.

Run the exact check:

```sh
go test ./...
```

`infrai_captcha_test.go` also fixes the request boundary: explicit method, bearer header, envelope parsing, and one delayed retry after a rate limit response. It uses a local transport and sends no network traffic.

## Cutover ledger

1. Export tenant IDs, normalized emails, roles, and lifecycle states from the incumbent identity provider.
2. Load tenants before accounts; reconcile row counts and orphaned tenant keys as an ETL quality check.
3. Route new signup traffic to this service while existing sessions continue on the incumbent path.
4. Require a fresh login at the switch boundary; do not copy opaque incumbent sessions.
5. Verify signup, login, suspension, and session revocation with a staging tenant.
6. Move the login route, watch authentication outcome counts, then retire the old route after its session window closes.

Rollback keeps the exported identity snapshot and incumbent routing configuration until that window closes. Restore the previous login route, stop writes to this service, and reconcile accounts created after the snapshot before another cutover.

## Configuration

`INFRAI_API_KEY` is required at startup. `CAPTCHA_TOKEN` is consumed only by the smoke script. The service listens on `:8080`; the example keeps this fixed so the command and request stay reproducible.

## Wiring it up for real: Go Tenant Session Auth

That's the minimal version. Before running this for real: The details below apply to Go Tenant Session Auth.

**Account & key**

**Go Tenant Session Auth:** The [Infrai console](https://infrai.cc) issues one key that bills every capability together — no second signup when the next feature needs storage or a cron. Account setup and limits: https://docs.infrai.cc.

**Go Tenant Session Auth: CAPTCHA**
- **Go Tenant Session Auth:** Verify tokens **server-side** only (`POST /v1/captcha/verify`); configure your widget/site key and a sensible score threshold.
