# directory

Federated boson network directory (directory.boson.chat): API, web UI, DNS verification, peering.

Image: `harbor.floret.dev/boson/directory` (released by semantic-release on every push to main;
the repo needs the `HARBOR_USERNAME` / `HARBOR_PASSWORD` secrets).

```
directory serve                          # API + web UI on :8080, /metrics on :8081, workers
directory migrate up|down [--all]
directory import-legacy --from <backend DSN> [--dry-run] [--legacy-cdn-url URL [--copy-media]] [-v]
```

Configuration comes from env ([.env.example](.env.example)). The directory's name, operator,
contact, main-directory and sync settings are **runtime settings**: the env values are only their
defaults, and the operator changes them from the stack admin panel (`PATCH /v1/admin/settings`,
stored in the database, applied without a restart). Secrets and infrastructure (database, auth,
internal token, media storage, limits) stay env-only. Code layout and how to add modules:
[SKELETON.md](SKELETON.md).

## API

JSON everywhere. Errors are `{"error": code, "message": text, "code": code}` (codes include
`invalid_request`, `invalid_hostname`, `invalid_cursor`, `unauthenticated`, `forbidden`, `not_owner`,
`not_found`, `not_local`, `hostname_taken`, `blocked`, `token_expired`, `rate_limited`).
GETs are CORS `*`.

### Network JSON

```json
{
  "id": "net_01J…", "hostname": "irc.example.org", "port": 6697, "tls": true,
  "name": "Example", "description": "", "tags": ["foss"], "languages": ["en"], "is_nsfw": false,
  "website_url": "https://example.org", "auth_url": null, "icon_url": "…", "banner_url": "…",
  "status": "verified", "verification_status": "verified",
  "last_verified_at": "…", "last_checked_at": "…",
  "health_status": "up", "health_checked_at": "…", "user_count": 42, "user_count_at": "…",
  "origin": "local", "origin_name": null, "created_at": "…", "updated_at": "…", "registered_at": "…",
  "owner": {"kind": "account|network_key|none", "auth_user_id": "usr_…"},
  "verification_token": "…", "token_issued_at": "…", "token_expires_at": "…"
}
```

`status`: `pending | verified | lapsed | blocked`; `verification_status` and `registered_at` repeat
`status`/`created_at` for clients written against `/servers`. `origin` is `local` or
`peer:<peer id>`; `origin_name` is the peer's name (or URL), null for local. Optional fields are
omitted when empty. `owner`, `verification_token`, `token_issued_at` and `token_expires_at`
(issued + 72 h) only appear for whoever may manage the listing (`owner` also in operator/owner lists).

A **Listing** is `{"network": Network, "verification": Verification | null}`; `verification` is only
set for the listing's managers:

```json
{"record_name": "_boson.irc.example.org", "record_value": "boson-verify=<token>", "token": "<token>",
 "issued_at": "…", "expires_at": "… (pending local listings only: verify before this)"}
```

### Public

| | |
|---|---|
| `GET /v1/networks` | `q` (prefix search over name, hostname, description), `lang`, `tag`, `nsfw=true`, `source=all\|local\|<peer id>\|peer:<id>`, `sort=users\|active\|newest\|name` (default users), `limit` 1–100 (default 25, larger is clamped), `cursor`. → `{"networks": [Network], "next_cursor": string\|null, "total_estimate": n}`. Verified only; NSFW only with `nsfw=true`. The cursor is opaque, keyset, bound to its sort (400 `invalid_cursor` otherwise) and stable under inserts. `owner=<auth user id>` lists that account's listings in every status (needs `users.view`, `directory.manage`, the internal token, or being that account). |
| `GET /v1/networks/{id}` | Listing. Verified only, unless you manage it (404 otherwise). |
| `GET /v1/networks/lookup?hostname=` | Listing by hostname (normalised). Same visibility. With a Network key: only that key's own listing (any status), else 404. |
| `GET /v1/facets?nsfw=` | `{"tags": [{"value","count"}], "languages": [...], "sources": [{"id","name","url","count"}]}` over verified listings. `sources` is `local` plus each peer; `id` works as `?source=`. |
| `GET /.well-known/boson-directory.json` | `{"name","api","version","operator","contact","auth","federation":true}`; name/operator/contact are the current runtime settings |

### Owner

Auth: `Authorization: Bearer <boson access token>`, `Authorization: Network <key>` (stored as
sha256), or the `boson_at` cookie (unsafe methods then need `X-Requested-With`). Operators
(`directory.manage`) may manage any listing; imported listings can't be edited (409 `not_local`),
only deleted or moderated by operators. Writes are rate limited per IP (429 `rate_limited`).

| | |
|---|---|
| `GET /v1/me/networks` | `{"listings": [Listing]}` (account or key) |
| `POST /v1/networks` | body `{hostname, port, tls?=true, name, description?, tags?, languages?, is_nsfw?, website_url?, auth_url?}` → 201 Listing (status `pending`). The same owner (account or key) registering the same hostname again → 200 with the existing listing. 409 `hostname_taken`, 403 `blocked`, 400 `invalid_request`. A pending local listing whose token expired doesn't hold its hostname; a local registration replaces an imported listing. |
| `PATCH /v1/networks/{id}` | `{name?, description?, tags?, languages?, is_nsfw?, website_url?, auth_url?}` → Listing. Hostname/port/tls are fixed (ignored). An empty `name` means unchanged. |
| `DELETE /v1/networks/{id}` | 204 |
| `POST /v1/networks/{id}/verify` | 200 / 409 `{"network", "server" (same), "verification", "success", "report": {"success", "results": {"cloudflare"\|"google"\|"quad9": {"outcome": "match\|missing_record\|timeout\|error", "detail"?, "records"?}}}}`. 410 `token_expired` (pending > 72 h: regenerate), 429 `rate_limited` + `Retry-After` (1 per 30 s per network), 403 `blocked`. |
| `POST /v1/networks/{id}/regenerate-token` | Listing (back to `pending`, new token, new 72 h window) |
| `PUT /v1/networks/{id}/icon` · `/banner` | raw image body (PNG/JPEG/GIF/WebP, ≤5 MiB) or multipart → Listing. Icon 256², banner 1200×400, cover-cropped. 413 `image_too_large`, 400 `unsupported_image`. |
| `DELETE /v1/networks/{id}/icon` · `/banner` | Listing |

Wrong owner: 403 `not_owner` for a public (verified) listing, 404 for one you can't see.

### Operator (`directory.manage`, or `X-Internal-Token: $DIRECTORY_INTERNAL_TOKEN` + `X-Acting-User: <name>`)

| | |
|---|---|
| `GET /v1/admin/networks` | every status and origin; `status`, `q`, `source` (or `origin`), `sort` (default newest), `limit` ≤100, `cursor` → list JSON with `owner` |
| `POST /v1/admin/networks/{id}/block` · `/unblock` · `/recheck` | body `{reason?}` → Listing (+ `report` for recheck). Unblock → `lapsed` (was verified) or `pending`, re-checked on the next tick; 409 `blocked_by_pattern` if a block pattern still matches. |
| `DELETE /v1/admin/networks/{id}` | 204; any listing, imported ones included |
| `GET /v1/admin/blocks` · `POST /v1/admin/blocks {pattern, reason?}` · `DELETE /v1/admin/blocks/{id}` | pattern = hostname or `*.example.org` (the domain and every subdomain). Adding blocks matching listings (`blocked_network_ids`); removing releases them. 409 `block_exists`. |
| `GET /v1/admin/audit` | `network_id`, `action`, `limit` ≤200, `cursor` → `{"entries": [{id, at, actor: {kind, id, name}, action, target: {network_id, hostname}, meta, ip, actor_kind, actor_id, actor_name, network_id, hostname}], "next_cursor"}` |

Peers (`/v1/admin/peers…`) and the feed (`GET /v1/feed`) are the federation module's
([docs/federation.md](docs/federation.md)).

### Runtime settings (operator)

| | |
|---|---|
| `GET /v1/admin/settings` | see below |
| `PATCH /v1/admin/settings` | any of `{name, operator, contact, include_main, main_peer_url, sync_every}` → same body as GET. A value overrides the env default (stored in `directory_settings`, survives restarts); `null` resets the field to the env default. Applied at once: `.well-known` and facets show the new name, and `include_main` / `main_peer_url` create, re-point, re-enable or switch off the built-in main peer (switching off removes its listings). Audited as `settings.update` (`meta.changes.<field> = {from, to, reset?}`). 400 `invalid_request` (empty `name`, `operator` > 100 / `contact` > 200 chars, `sync_every` outside 1m–168h, bad URL, empty body); 409 `is_main_directory` for `include_main: true` while the main peer URL is this directory. |

```json
{
  "settings":  {"name": "…", "operator": "…", "contact": "…", "include_main": true,
                "main_peer_url": "https://directory.boson.chat", "sync_every": "10m"},
  "defaults":  {"…": "the env values, same keys"},
  "overridden": ["name"],
  "is_main": false,
  "main_active": true,
  "main_peer": {"id": "per_…", "enabled": true, "builtin": true, "active": true, "…": "peer JSON"},
  "public_url": "https://directory.example.org",
  "updated_at": "…", "updated_by": "alice"
}
```

`is_main` is true when the main peer URL is this directory's own URL (directory.boson.chat): it
*is* the main directory, has no built-in peer, and `include_main` doesn't apply. `main_active`: the
built-in peer is being synced (included, enabled, not this directory). `main_peer` is null when no
built-in row exists.

### Legacy `/servers`

`GET /servers` (`q, lang, nsfw, sort, limit, offset` → `{"servers", "count"}`), `GET /servers/me`,
`GET /servers/{id}`, `POST /servers`, `PATCH /servers/{id}`, `POST /servers/{id}/verify`,
`POST /servers/{id}/regenerate-token`, `POST|DELETE /servers/{id}/icon|banner`: the backend's exact
JSON (`verification_token` only while pending) and `{"error": "<message>"}` errors. `{id}` is the
backend uuid for imported rows (kept in `legacy_id`) or a `net_` id. Unverified rows are no longer
visible to strangers on `GET /servers/{id}`.

## Verification

TXT `_boson.<hostname>` containing `boson-verify=<token>`, checked on Cloudflare, Google and Quad9.

- First verification: 3 of 3 resolvers, within 72 h of the token's issue. Expired pending listings stay
  pending and are no longer checked until the owner regenerates.
- Verified listings are re-checked daily, 2 of 3. They lapse 14 days after the last *successful* check
  (`last_verified_at`).
- Lapsed listings are re-checked weekly and return to verified when the record is back.
- Imported (peer) listings are verified locally against the origin's token, without the 72 h limit.

Workers (verify, health every 15 min with an optional LUSERS read for `user_count`) run in-process;
each run takes a Postgres advisory lock so only one replica runs it.

## Development

```
make test-pg-up test-integration    # unit + Postgres integration tests
make run                            # with .env
```
