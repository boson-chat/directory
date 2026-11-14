# directory skeleton

How the service is put together and how to add to it. The plan
(`glistening-doodling-beaver.md`) is the contract; this file maps it onto code.

## Layout

```
cmd/main.go                      cobra entry (serve, migrate up|down, import-legacy)
config/config.go                 every env var (configor, env tags)
db/migration.go                  embedded golang-migrate migrations
db/migrations/000001_initial.*   networks, network_tombstones, peers, blocks, audit
http/server.go                   Build(): DB → repos → services → modules; Serve()
http/modules.go                  blank-imports the modules (add yours here)
http/router/                     Module registry, Deps, Router (access levels), JSON helpers
http/middleware/                 Recover, ClientIP, Logging, SecurityHeaders, CORS, Authenticate
http/handlers/                   core module: well-known, public/owner/operator/legacy API, verify+health workers
http/spa/                        serves ui/build (SPA fallback, CSP with inline-script hashes)
ui/embed.go                      //go:embed all:build (ui/build/.gitkeep keeps it compiling)
internal/principal/              the caller: account (JWT, perms) or Network key (sha256 of key)
internal/db/db.go                GORM + WithTx (ctx-scoped transactions)
internal/db/repositories/network networks repo: CRUD, keyset List, search, facets,
                                 verify/health bookkeeping, UpsertByOrigin, Changes (feed)
internal/db/repositories/{block,audit,peer}
internal/services/networks       NetworkServiceImpl: public, owner, verification state machine
internal/services/dnsverify      Verifier interface (+ real 3-resolver impl, Fake)
internal/services/{blocks,audit,media,health,workers}
internal/leader                  pg advisory-lock leader per job
metrics/                         OTel instruments + Prometheus/OTLP provider
```

## Adding routes / workers (federation, BFF, …)

JSON for listings: `http/views` (`views.NewNetwork`, `views.NewListing`) — reuse it for the feed's
public network fields. The API reference is in README.md.

Create a package (e.g. `http/handlers/federation`) and register a module:

```go
func init() {
	router.Register(router.Module{
		Name: "federation",
		Routes: func(d *router.Deps, r *router.Router) error {
			h := &handler{d: d, svc: federation.New(d)}
			r.Public("GET /v1/feed", h.feed)               // anyone (CORS *)
			r.Operator("GET /v1/admin/peers", h.listPeers)  // needs directory.manage
			return nil
		},
		Workers: func(d *router.Deps) []workers.Worker {
			return []workers.Worker{{Name: "federation-sync", Every: d.Config.FederationConfig.SyncEvery, Run: svc.SyncAll}}
		},
		Start: func(ctx context.Context, d *router.Deps) error { return svc.EnsureBuiltinPeer(ctx) },
	})
}
```

then add `_ "github.com/boson-chat/directory/http/handlers/federation"` to
`http/modules.go`. Rules:

- **Always use method patterns** (`"GET /v1/…"`): the SPA is mounted at `GET /`,
  and a method-less pattern conflicts with it (ServeMux panics).
- `router.Router` access levels: `Public`, `Authed` (account or Network key),
  `Account` (account only), `Operator(pattern, h, perm...)` (default `directory.manage`).
- `principal.From(r.Context())` is the caller (nil = anonymous) on every route.
- Respond with `router.WriteJSON`, `router.WriteError` (renders `apperr.Error`
  as `{"error": code, "message": text, "code": code}`, anything else as a logged 500),
  `router.Decode` (requires `Content-Type: application/json`).
- Workers run under `leader.LockerImpl` (pg advisory lock keyed by worker name),
  so only one replica runs a job at a time. `App.RunWorkerOnce(ctx, name)` runs one now (tests).
- Operator API paths live under `/v1/admin/…` (peers: `/v1/admin/peers…`).

## Deps (http/router.Deps)

`Config, DB, NetworkRepo, Networks, Blocks, Audit, Media, Verifier, Locker, Metrics, HTTPClient, Now`.

### Network repository (federation-relevant)

- `UpsertByOrigin(ctx, n)` with `n.Origin = network.PeerOrigin(peerID)` and `n.OriginID` = id at the peer.
  Writes the public fields + `verification_token`. Conflict rules are enforced here:
  a local listing always keeps its hostname; another peer's **verified** (or blocked) listing keeps it;
  another peer's unverified listing is replaced. Returns `created|updated|unchanged|conflict`.
  A changed token resets the row to `pending` (it must re-verify locally). New rows start `pending`;
  the verify worker checks them (strict 3/3) on its next tick. **Check `Blocks.Match` before upserting.**
- `DeleteByOrigin`, `DeleteAllByOrigin`, `CountByOrigin`.
- `Changes(ctx, since, limit)`: networks and tombstones with `seq > since`, in seq order.
  seq assignment is serialised with commit order (advisory lock in the trigger), so a reader that
  has seen seq N never misses a later commit with a smaller seq.
- `seq` moves on every real change (insert, update of any field other than the probe timestamps
  `last_checked_at/last_verified_at/health_checked_at/user_count_at`, delete → tombstone).
  `health_status`/`user_count` changes DO bump it.

### Verification (internal/services/networks)

- `dnsverify.Verifier.Verify(ctx, hostname, token, ModeStrict|ModeLenient)` → `Report{success, results{provider: {outcome, detail, records}}}`.
  Record: `_boson.<hostname>` TXT containing `boson-verify=<token>`.
- `Networks.CheckOne(ctx, n)` runs and records the check the worker would run (mode from status).
- Imported (peer) pending listings are not subject to the 72h token TTL (the token is the origin's).
- `DNS_VERIFIER=fake` (refused with `ENV=production`) → every check succeeds. Tests use
  `dnsverify.NewFake()` with `Set(host, matches)` / `SetToken(host, token)`.

## Auth

- `Authorization: Bearer <auth access token>` (EdDSA, JWKS from `AUTH_JWKS_URL`, `typ at+jwt`,
  iss `AUTH_ISSUER`, aud `AUTH_AUDIENCE`=boson). Claims used: `sub` (usr_…), `name`, `role`, `perms`.
- `Authorization: Network <key>` → principal with `KeyHash = sha256hex(key)`. The admin panel derives
  the key as `base64url(HMAC-SHA256(secret, "boson-admin directory key v1\x00" + lower(hostname)))`.
- Cookie `boson_at` (`AUTH_ACCESS_COOKIE`) for the browser flow when the auth cookie is scoped to the
  parent domain; unsafe methods then need `X-Requested-With`.
- `X-Internal-Token: $DIRECTORY_INTERNAL_TOKEN` (+ `X-Acting-User: <name>` for the audit log) →
  `principal.KindInternal`: an operator with every permission (`p.Has(anything)` is true), accepted by
  `router.Router.Operator`. Used by the stack admin panel. Internal principals can't own listings.
- CORS: `Access-Control-Allow-Origin: *` for everyone (no cookies); origins in `CORS_ALLOWED_ORIGINS`
  get the credentialed form.

## Tests

`TEST_DATABASE_URL` → Postgres integration tests (each test creates its own database).
`make test-pg-up test-integration`.
