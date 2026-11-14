# Federation

A directory imports other directories' listings ("peers") and exports its
own through a change feed. Trust always comes from DNS: an imported listing
only shows as verified once *this* directory has found the token in the
network's `_boson.<hostname>` TXT record itself.

## Feed

`GET /v1/feed?since=<seq>&limit=<1..1000, default 500>` (public, CORS `*`)

```json
{
  "changes": [
    {"seq": 41, "network": {"id": "net_…", "hostname": "irc.example.org", "port": 6697, "tls": true,
      "name": "…", "description": "…", "tags": ["…"], "languages": ["en"], "is_nsfw": false,
      "website_url": "https://…", "auth_url": "https://…", "icon_url": "https://…", "banner_url": "https://…",
      "created_at": "…", "updated_at": "…"},
     "verification_token": "…", "deleted": false},
    {"seq": 42, "network": {"id": "net_…"}, "deleted": true}
  ],
  "next_since": 42,
  "has_more": false
}
```

- Keep `next_since` as the cursor; continue while `has_more` is true.
- **Only listings registered on this directory are exported** (origin
  `local`). Imported listings are never re-exported: no A→B→A loops, no
  transitive trust. To see C's listings, peer with C.
- Only `verified` listings are exported in full. A local listing that is
  pending, lapsed or blocked, or deleted, is sent as `deleted: true` with
  just its id (unverified hostnames are not public).
- Owner data (account, network key hash) and probe data are never exported.

## Peers (operator API, `directory.manage`)

| | |
|---|---|
| `GET /v1/admin/peers` | `{peers:[{id,url,name,api,enabled,builtin,active,cursor,last_sync_at,last_full_sync_at,last_error,listing_count,created_at,updated_at}]}` |
| `GET /v1/admin/peers/check?url=` | `{url, well_known}`: normalises the URL and validates `/.well-known/boson-directory.json` without saving |
| `POST /v1/admin/peers` `{url, name?}` | 201 `{peer}`; 400 `self_peer`, 409 `peer_exists`, 502 `peer_unreachable` |
| `GET /v1/admin/peers/{id}` | `{peer}` |
| `PATCH /v1/admin/peers/{id}` `{enabled?, name?}` | `{peer}`; disabling removes the peer's listings and resets its cursor |
| `DELETE /v1/admin/peers/{id}` | 204; removes its listings; 409 `builtin_peer` for the main peer (disable it instead) |
| `POST /v1/admin/peers/{id}/sync` | 200 `{peer, result}` (counts: created, updated, deleted, conflicts, blocked, rejected, skipped, verified); 409 `sync_in_progress` / `peer_disabled`. Capped at 45 s; a cut-short sync resumes from the saved cursor |

Errors are `{error, code, message}`.

## Sync

Every `DIRECTORY_SYNC_EVERY` (10 min) the `federation-sync` worker (one
replica at a time, Postgres advisory lock; each peer also has its own lock
shared with "sync now") pulls each active peer:

1. Fetch and validate the peer's `.well-known`. If it advertises this
   directory's own API, nothing is imported.
2. Walk the feed from the saved cursor (from 0 on the first sync and every
   `DIRECTORY_PEER_RESYNC_EVERY`, 24 h; a full walk also removes listings
   the peer no longer exports, e.g. after it pruned tombstones). The cursor
   is saved after every page.
3. Every field goes through the same validators as a local registration;
   invalid entries are rejected (and an older copy dropped).
4. Blocks apply: blocked hostnames are not imported, and an existing import
   that becomes blocked is removed on the next sync.
5. Upsert with `origin=peer:<id>` and the peer's token. Conflicts: a local
   listing always keeps its hostname; between peers the first **verified**
   listing wins (an unverified one is replaced). A new token sends the row
   back to `pending`.
6. New/changed pending imports get this directory's own DNS check right
   away (lenient, 2 of 3 resolvers). Whatever isn't confirmed stays pending
   and the verify worker keeps checking it.

## Built-in main peer

- `DIRECTORY_MAIN_PEER` (default `https://directory.boson.chat`) is created
  on start as a `builtin` peer when `DIRECTORY_INCLUDE_MAIN=true` (default).
- An operator disabling it (`PATCH {enabled:false}`) is stored on the row
  and survives restarts; it is never silently re-enabled or deleted.
- `DIRECTORY_INCLUDE_MAIN=false` hides it from the API and removes its
  listings; the row (and the operator's choice) is kept for when it is
  turned back on. Changing `DIRECTORY_MAIN_PEER` re-points the row.
- A directory never peers with itself: `PUBLIC_URL` / `DIRECTORY_PUBLIC_URL`
  equal to the peer URL, or the peer's `.well-known` `api` pointing back
  here, both count. directory.boson.chat therefore has no builtin peer.
- `DIRECTORY_PEERS` (comma-separated URLs) seeds extra peers on start
  (only if missing).

## Safety

- Peer URLs: https only and public IP addresses only when `ENV=production`
  (checked after DNS resolution on every connection, so DNS rebinding can't
  reach internal services; no proxy env, ≤3 redirects, https-only
  redirects). Outside production http and private addresses are allowed
  for local testing.
- `DIRECTORY_PEER_TIMEOUT` (15 s) per request, `DIRECTORY_PEER_SYNC_TIMEOUT`
  (5 min) per sync, `DIRECTORY_PEER_MAX_BYTES` (8 MiB) per response,
  `DIRECTORY_PEER_MAX_LISTINGS` (10000) per peer (the excess is skipped and
  reported in `last_error`).
- Imported image URLs must be https in production.

## Configuration

| env | default | |
|---|---|---|
| `DIRECTORY_MAIN_PEER` | `https://directory.boson.chat` | built-in main peer |
| `DIRECTORY_INCLUDE_MAIN` | `true` | include it |
| `DIRECTORY_PEERS` | | extra peer URLs seeded on start |
| `DIRECTORY_PUBLIC_URL` | | extra self URL (besides `PUBLIC_URL`) |
| `DIRECTORY_SYNC_EVERY` | `10m` | worker interval |
| `DIRECTORY_SYNC_ENABLED` | `true` | run the worker |
| `DIRECTORY_PEER_TIMEOUT` | `15s` | |
| `DIRECTORY_PEER_SYNC_TIMEOUT` | `5m` | |
| `DIRECTORY_PEER_MAX_BYTES` | `8388608` | |
| `DIRECTORY_PEER_MAX_LISTINGS` | `10000` | |
| `DIRECTORY_PEER_RESYNC_EVERY` | `24h` | full feed walk |
| `DIRECTORY_PEER_VERIFY_CONCURRENCY` | `8` | parallel DNS checks of imports |

## Tests

- `go test ./internal/services/federation` — URL/SSRF guard, limits, feed
  building, import validation.
- `make test-pg-up test-integration` — several in-process directories on
  Postgres: feed paging, import + re-verification, conflicts, blocks,
  deletes, full-resync sweep, limits, built-in peer persistence, self-peer.
- `make test-federation` — two directories, Postgres and a fake DNS/auth
  server in docker (`test/federation`), verifying over real DNS queries.
