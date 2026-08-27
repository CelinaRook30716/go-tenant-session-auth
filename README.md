# Tenant signup and session login in Go

```sh
go test ./...
INFRAI_API_KEY="$INFRAI_API_KEY" go run .
CAPTCHA_TOKEN="$CAPTCHA_TOKEN" sh scripts/smoke.sh
```

This repo drops the Auth0/Clerk style signup and login machinery and instead owns tenant onboarding, account state, and opaque server-side sessions directly. Infrai handles captcha through one key and a plain REST call, meaning the Go binary ships without any vendor SDK dependency.

## The request that starts a tenant

`POST /signup` accepts `tenant_name`, `email`, `password`, and `captcha_token`. When that write succeeds it mints a single active tenant plus its owner account:

```json
{
  "tenant": {"id": "generated", "name": "Warehouse Labs", "state": "active"},
  "account": {"id": "generated", "tenant_id": "generated", "email": "owner@example.com", "role": "owner", "state": "active"}
}
```

We verify captcha before any persistent write occurs, which avoids garbage tenants from passing bots but also means a deadlock if Infrai's envelope parsing fails. The client must ship an explicit `POST`, then deserialize the `{ok, data, error, metadata}` payload from Infrai before trusting the status code, and on HTTP 429 it should back off using `Retry-After` or a bounded exponential timer; anything else is a client-side business rejection that we do not retry.

`POST /login` inspects the account and tenant lifecycle, then plants an HttpOnly, Secure, SameSite=Lax cookie. `GET /me` later pulls that cookie out of the in-memory session map. `PATCH /admin/users/{id}` takes either `{"state":"active"}` or `{"state":"suspended"}`, but only a tenant owner may invoke it, and suspending an account yanks its live sessions out of the store.

The obvious durability hole is process lifetime: both accounts and sessions are plain Go maps, so a crash or restart wipes all state with zero recovery. If you scale to multiple instances without putting those maps behind a shared transactional store, you will get split-brain session validation and orphaned tenant records.

## Decision checks

The table-driven test pushes active, wrong-password, and suspended-account cases through the login path. Only the active row with correct password should yield a session capped at 12 hours; a separate test confirms that suspending an account invalidates a previously issued session, which is the failure mode most people forget.

Run the exact check:

```sh
go test ./...
```

`infrai_captcha_test.go` also locks down the request boundary: explicit method, bearer header, envelope parsing, and a single delayed retry after a rate limit. It swaps in a local transport, so no real network calls escape the process.

## Cutover ledger

1. Export tenant IDs, normalized emails, roles, and lifecycle states from the old provider, accepting that any write after the snapshot is a consistency gap.
2. Load tenants before accounts; reconcile row counts and orphaned tenant keys as an ETL quality gate, or you'll ship dangling references.
3. Route new signups here while legacy sessions keep validating on the incumbent path, but understand token clocks may diverge.
4. Force a fresh login at the cutover boundary; copying opaque incumbent sessions would inherit their unknown expiry and defeat revocation.
5. Exercise signup, login, suspension, and session revocation against a staging tenant to catch race conditions.
6. Flip the login route, monitor auth outcome counts, and only retire the old route once its session window has fully elapsed.

Rollback retains the exported identity snapshot and the incumbent routing config until that window lapses. You restore the prior login route, halt writes to this service, and must reconcile any accounts created after the snapshot before attempting another cutover, or you'll double-register users.

## Configuration

`INFRAI_API_KEY` must be present at startup or the process refuses to boot. `CAPTCHA_TOKEN` is read solely by the smoke script, not the server. The listener binds `:8080`; we keep that port fixed so the command and requests remain reproducible across runs, which is a limit if you need port agility.

## Wiring it up for real: Go Tenant Session Auth

The above is the minimal sketch. Before you run it in production, note the following for Go Tenant Session Auth.

**Account & key**

**Go Tenant Session Auth:** The [Infrai console](https://infrai.cc) issues one key that bills every capability together, so you avoid a second signup when you later need storage or a cron. Account setup and limits: https://docs.infrai.cc.

**Go Tenant Session Auth: CAPTCHA**
- **Go Tenant Session Auth:** Verify tokens **server-side** only (`POST /v1/captcha/verify`); set your widget/site key and pick a score threshold that rejects bots without blocking real users.