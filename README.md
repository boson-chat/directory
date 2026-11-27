# directory

Federated boson network directory (directory.boson.chat): API, web UI, DNS verification, peering.

Image: `harbor.floret.dev/boson/directory` (released by semantic-release on every push to main;
the repo needs the `HARBOR_USERNAME` / `HARBOR_PASSWORD` secrets).

```
directory serve                          # API + web UI on :8080, /metrics on :8081, workers
directory migrate up|down [--all]
directory import-legacy --from <backend DSN> [--dry-run] [--legacy-cdn-url URL [--copy-media]] [-v]
directory export-config                  # print the directory: section for config/boson.yaml (env + database)
```

Configuration comes from env ([.env.example](.env.example)). The directory's name, operator,
contact, main-directory and sync settings are **runtime settings**: the env values are only their
defaults, and the operator changes them from the stack admin panel (`PATCH /v1/admin/settings`,
stored in the database, applied without a restart). Secrets and infrastructure (database, auth,
internal token, media storage, limits) stay env-only. With the [stack config
file](#stack-config-file) (`BOSON_CONFIG`) those settings and the peer list come from its
`directory:` section instead. Code layout and how to add modules: [SKELETON.md](SKELETON.md).

## API

JSON everywhere. Errors are `{"error": code, "message": text, "code": code}` (codes include
`invalid_request`, `invalid_hostname`, `invalid_cursor`, `unauthenticated`, `forbidden`, `not_owner`,
`not_found`, `not_local`, `hostname_taken`, `blocked`, `token_expired`, `rate_limited`, `not_verified`,
`no_pending_move`, `move_stale`).
GETs are CORS `*`.

### Network JSON

```json
{
  "id": "net_01J…", "hostname": "irc.example.org", "port": 6697, "tls": true,
  "name": "Example", "description": "", "tags": ["foss"], "languages": ["en"], "is_nsfw": false,
  "website_url": "https://example.org", "auth_url": null, "icon_url": "…", "banner_url": "…",
  "theme": {"version": 1, "base": "dark", "colors": {"accent": "#7c3aed"}, "logo": "https://…"} | null,
  "channels": ["#lobby", "#help"], "autojoin_channels": ["#lobby"],
  "previous_hostnames": ["irc.old.example.org"],
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

A **Listing** is `{"network": Network, "verification": Verification | null, "move"?: Move,
"moved_from"?: string}`; `verification` and `move` (the pending [move to a new
hostname](#moving-a-listing-to-a-new-hostname)) are only set for the listing's managers, `moved_from` only by
a lookup by an old hostname. `previous_hostnames` (always present, `[]` when it never moved) lists the
hostnames the listing moved away from, most recent first:

```json
{"record_name": "_boson.irc.example.org", "record_value": "boson-verify=<token>", "token": "<token>",
 "issued_at": "…", "expires_at": "… (pending local listings only: verify before this)"}
```

### Public

| | |
|---|---|
| `GET /v1/networks` | `q` (prefix search over name, hostname, description), `lang`, `tag`, `nsfw=true`, `source=all\|local\|<peer id>\|peer:<id>`, `sort=users\|active\|newest\|name` (default users), `limit` 1–100 (default 25, larger is clamped), `cursor`. → `{"networks": [Network], "next_cursor": string\|null, "total_estimate": n}`. Verified only; NSFW only with `nsfw=true`. The cursor is opaque, keyset, bound to its sort (400 `invalid_cursor` otherwise) and stable under inserts. `owner=<auth user id>` lists that account's listings in every status (needs `users.view`, `directory.manage`, the internal token, or being that account). |
| `GET /v1/networks/{id}` | Listing. Verified only, unless you manage it (404 otherwise). `{id}` may also be a listing's legacy id (the uuid it had on the boson API directory; imported rows show it as `legacy_id`). |
| `GET /v1/networks/lookup?hostname=` | Listing by hostname (normalised). Same visibility. With a Network key: only that key's own listing (any status), else 404. A hostname no visible listing has now, but that a listing [moved away from](#moving-a-listing-to-a-new-hostname), returns that listing (200, its current hostname) with `"moved_from": "<the hostname asked for>"` and `Content-Location: /v1/networks/{id}`. |
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
| `POST /v1/networks` | body `{hostname, port, tls?=true, name, description?, tags?, languages?, is_nsfw?, website_url?, auth_url?, channels?, autojoin_channels?}` → 201 Listing (status `pending`). The same owner (account or key) registering the same hostname again → 200 with the existing listing. 409 `hostname_taken`, 403 `blocked`, 400 `invalid_request`. A pending local listing whose token expired doesn't hold its hostname; a local registration replaces an imported listing. |
| `PATCH /v1/networks/{id}` | `{name?, description?, tags?, languages?, is_nsfw?, website_url?, auth_url?, channels?, autojoin_channels?}` → Listing. Hostname/port/tls are ignored here (see `/move`). An empty `name` means unchanged. A list replaces the stored one (`[]` clears it; absent or `null` keeps it). 400 `invalid_channels` / `invalid_autojoin_channels` (see [Network channels](#network-channels)). |
| `DELETE /v1/networks/{id}` | 204 |
| `POST /v1/networks/{id}/verify` | 200 / 409 `{"network", "server" (same), "verification", "success", "report": {"success", "results": {"cloudflare"\|"google"\|"quad9": {"outcome": "match\|missing_record\|timeout\|error", "detail"?, "records"?}}}}`. 410 `token_expired` (pending > 72 h: regenerate), 429 `rate_limited` + `Retry-After` (1 per 30 s per network), 403 `blocked`. |
| `POST /v1/networks/{id}/regenerate-token` | Listing (back to `pending`, new token, new 72 h window) |
| `PUT /v1/networks/{id}/icon` · `/banner` | raw image body (PNG/JPEG/GIF/WebP, ≤5 MiB) or multipart → Listing. Icon 256², banner 1200×400, cover-cropped. 413 `image_too_large`, 400 `unsupported_image`. |
| `DELETE /v1/networks/{id}/icon` · `/banner` | Listing |
| `PUT /v1/networks/{id}/theme` | JSON body: a [boson-theme/1](#listing-themes) document → Listing. 400 `invalid_theme`. |
| `DELETE /v1/networks/{id}/theme` | Listing (theme `null`, uploaded theme images removed) |
| `PUT /v1/networks/{id}/theme-images/{slot}` | slot `logo` · `logo_wide` · `login_background` · `pane-servers` · `pane-channels` · `pane-messages` · `pane-members`; raw image body or multipart (PNG/JPEG/GIF/WebP/SVG, ≤2 MB, ≤4096 px a side; not cropped) → Listing with the URL in `theme`. 413 `image_too_large`, 400 `unsupported_image`, 400 `unsafe_svg`, 404 `unknown_slot`. |
| `DELETE /v1/networks/{id}/theme-images/{slot}` | Listing |
| `GET /v1/networks/{id}/move` | Listing + `"history": [Move]` (newest first, at most 20) |
| `POST /v1/networks/{id}/move` | `{hostname, port?, tls?}` (port/tls default to the current ones) → 201 Listing with the pending `move`. See [below](#moving-a-listing-to-a-new-hostname). |
| `POST /v1/networks/{id}/move/verify` | 200 `{network, verification, move: null, moved: true, success, report}` once moved; 409 same body with `moved: false`, the pending `move` and the per-resolver `report`. 404 `no_pending_move`, 410 `token_expired`, 429 `rate_limited` + `Retry-After`, 409 `hostname_taken` / `move_stale`, 403 `blocked`. |
| `DELETE /v1/networks/{id}/move` | Listing (the pending move cancelled; 200 without one too) |

Wrong owner: 403 `not_owner` for a public (verified) listing, 404 for one you can't see.

### Moving a listing to a new hostname

Ownership is proven per hostname, so a listing can't just be edited to a new address. Instead its
manager (the owner's account by Bearer token or the `boson_at` cookie, the listing's Network key as the
stack admin panel uses, or an operator) **moves** it: the listing keeps its id, history, stats,
channels, tags, theme, images, owner and verified status. Typical case: putting Boson in front of an
existing ircd (`irc.example.org` → `irc.boson.example.org`), since Boson finds a network's
`/.well-known/boson.json` from its listed hostname.

1. `POST /v1/networks/{id}/move {"hostname": "irc.boson.example.org", "port": 6697, "tls": true}` → 201.
   Only a `verified` or `lapsed` local listing can move (409 `not_verified` for a pending one: delete
   and re-register instead; 409 `not_local` for imports; 403 `blocked`). The response's `move` carries a
   new token and the record for the new hostname:

   ```json
   {"id": "mov_01J…", "status": "pending",
    "from": {"hostname": "irc.example.org", "port": 6697, "tls": true},
    "to": {"hostname": "irc.boson.example.org", "port": 6697, "tls": true},
    "hostname": "irc.boson.example.org", "port": 6697, "tls": true,
    "record_name": "_boson.irc.boson.example.org", "record_value": "boson-verify=<token>", "token": "<token>",
    "requested_at": "…", "issued_at": "…", "expires_at": "… (issued + 72 h)", "last_checked_at": "…",
    "expired": false}
   ```

   Until it verifies, nothing public changes: the listing serves the old hostname, the feed and
   `seq` don't move, and the new hostname isn't shown to anyone else. The same request again returns
   the same move (and token); a different target replaces it with a new token. The same hostname with
   only a new port/tls applies at once (200, no DNS proof needed).
2. Publish the TXT record at the new hostname and `POST /v1/networks/{id}/move/verify`. Like a first
   verification it needs 3 of 3 resolvers, within 72 h of the token's issue, at most once per 30 s per
   listing. The verify worker also checks pending moves on its own every `VERIFY_PENDING_EVERY` (1 h).
3. On success, in one transaction: hostname/port/tls switch, the move's token becomes the listing's
   verification token (the daily re-check then uses the new record), the status is `verified` (a lapsed
   listing too), the old hostname is prepended to `previous_hostnames` (at most 10; moving back drops
   it), the listing is due for a health probe of the new address, and the move is `completed`. The old
   record can go.

Rules:

- **Uniqueness** follows registration: the new hostname can't be another listing's (409
  `hostname_taken`) unless that is an imported listing or a local claim that was never verified and
  whose token expired; those are removed when the move completes. Checked when requested and again
  at completion (someone who verifies the hostname first wins). Blocked hostnames: 403 `blocked`.
- **Expiry**: a move whose token is older than 72 h is no longer checked and `/move/verify` answers 410
  `token_expired`; `move.expired` is true. Request it again for a fresh token, or cancel. 30 days later
  the worker closes it (`expired`).
- **Lookups**: `GET /v1/networks/lookup?hostname=<old>` returns the moved listing with `moved_from`
  (an exact, visible listing at that hostname wins). `GET /v1/networks/{id}` is unchanged.
- **Peers** get an update of the same listing (same id) with the new hostname, the new token and
  `previous_hostnames` ([docs/federation.md](docs/federation.md#moves)).
- **Stack admin panel** (follow-up): with its `Authorization: Network <key>` it finds its listing with
  `GET /v1/networks/lookup?hostname=<irc host>` (the old hostname still resolves, with `moved_from`), reads
  the pending move from the Listing's `move`, and drives `POST …/move`, `POST …/move/verify` and `DELETE
  …/move`. A browser session uses the `boson_at` cookie and must send `X-Requested-With` on the writes.
- **Audit**: `network.move_request`, `network.move_check`, `network.move_cancel`, `network.move` (meta
  `from`/`to` `{hostname, port, tls}`, actor `system`/`verify` when the worker completed it), plus
  `network.replace_imported` / `network.replace_expired` for a holder it removed. `network_moves` keeps
  every request (`GET …/move` → `history`).

### Listing themes

`theme` is the network's boson-theme/1 branding, the same document the stack serves at
`/v1/branding` and the client applies when connected:

```json
{"version": 1, "base": "dark",
 "colors": {"accent": "#7c3aed", "bg": "#0b0b10"},
 "nick": {"saturation": 62, "lightness": 64}, "radius": 2, "font": "boson",
 "panes": {"servers": {"bg": "#0b0b10"},
           "messages": {"image": "https://…", "dim": 0.7, "blur": 8, "position": "center"}},
 "logo": "https://…", "logo_wide": "https://…", "login_background": "https://…",
 "variants": {"light": {"colors": {"bg": "#f5f9f6", "fg": "#0f1a14", "accent": "#0d7a45"},
                        "nick": {"saturation": 60, "lightness": 36},
                        "panes": {"servers": {"bg": "#e7efea", "dim": 0.4}}}}}
```

- `colors` keys are the client's theme tokens (`bg surface surface-raised surface-hover border
  border-soft fg fg-2 fg-3 muted meta accent accent-on success warn danger info`); others are
  dropped. Colours are `#rrggbb` (stored lowercase).
- `base` dark|light (default dark), `nick.*` 0–100, `radius` 0–12, `font` boson|system; panes
  `servers|channels|messages|members` with `bg`, `image`, `dim` 0–1, `blur` 0–24, `position`
  center|top|bottom|left|right. Unknown keys are dropped; at most 16 KB.
- `variants` (optional) carries the version for the other base: only `variants.<other base>` is
  kept (one equal to `base` is dropped). It has `colors`, `nick` and `panes` with the same rules;
  its panes override the primary's field by field (a pane's image is the primary's unless the
  variant gives an https `image`). `{}` means Boson's own look for that base. `radius`, `font` and
  the images stay top-level and shared. Readers pick the variant matching their dark/light mode
  and otherwise use the primary.
- Images are uploaded per slot (above) and stored on this directory's media host. In `PUT
  /theme` an image field that is omitted or `null` keeps the current image, `""` removes it, the
  URL `GET` returned keeps it, and any other https URL is hot-linked as is (how a stack pushes
  its `/v1/branding` assets with its Network key). http URLs only outside production.
- The federation feed carries `theme`; peers validate it again and hot-link its images, like
  icons and banners. An invalid theme is dropped (the listing is still imported).

### Network channels

`channels` are the network's fixed channels and `autojoin_channels` the ones new users are joined
to (the stack's `irc.channels`, all of them and those with `autojoin: true`). Both are `string[]`,
always present in the Network JSON (`[]` when none), and set by the owner (account or Network key,
which is how the stack admin pushes them) with `POST` / `PATCH /v1/networks/{id}`.

- At most 50 each. A name is `#` then 1–63 printable ASCII characters other than space and comma
  (surrounding whitespace is trimmed). Anything else is a 400 `invalid_channels` /
  `invalid_autojoin_channels` for the whole request.
- Duplicates are dropped ignoring ASCII case; the first spelling and the order are kept.
  `autojoin_channels` doesn't have to be a subset of `channels`.
- Listing cards show "Joins #lobby" from `autojoin_channels`; the network page lists `channels`.
  Boson joins a directory card's `autojoin_channels` on the first connect to that network.
- The federation feed carries both (omitted when empty). Peers validate them again; a peer
  that doesn't send them (an older directory) means none, and an invalid list is dropped without
  rejecting the listing.
- Rows from before migration 000006 read as `[]`.

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
| `GET /v1/admin/settings` | see below; `source` is `file` (stack config file) or `db` |
| `PATCH /v1/admin/settings` | 409 `managed_by_config_file` while the [stack config file](#stack-config-file) is in use. Otherwise any of `{name, operator, contact, include_main, main_peer_url, sync_every}` → same body as GET. A value overrides the env default (stored in `directory_settings`, survives restarts); `null` resets the field to the env default. Applied at once: `.well-known` and facets show the new name, and `include_main` / `main_peer_url` create, re-point, re-enable or switch off the built-in main peer (switching off removes its listings). Audited as `settings.update` (`meta.changes.<field> = {from, to, reset?}`). 400 `invalid_request` (empty `name`, `operator` > 100 / `contact` > 200 chars, `sync_every` outside 1m–168h, bad URL, empty body); 409 `is_main_directory` for `include_main: true` while the main peer URL is this directory. |

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
  "updated_at": "…", "updated_by": "alice",
  "source": "db",
  "peers": ["https://dir.example.net"]
}
```

In file mode `overridden` lists the keys set in the file, `peers` is the file's
`directory.peers` (omitted when that key is absent), and `updated_at` / `updated_by` are unset.

`is_main` is true when the main peer URL is this directory's own URL (directory.boson.chat): it
*is* the main directory, has no built-in peer, and `include_main` doesn't apply. `main_active`: the
built-in peer is being synced (included, enabled, not this directory). `main_peer` is null when no
built-in row exists.

### Stack config file

`BOSON_CONFIG=/config/boson.yaml` points at the boson stack's shared, non-secret config file.
The directory reads its `directory:` section and ignores the others. While the file exists, it is
the source of truth: **file > env > built-in default**. `directory_settings` is ignored, and the
settings API is read-only. A key that is absent (or `null`) falls back to its env value, so removing
a key resets it. Without `BOSON_CONFIG`, or while the file does not exist, the directory behaves
exactly as without it.

```yaml
version: 1
directory:
  name: Boson directory                       # DIRECTORY_NAME
  operator: Boson                             # DIRECTORY_OPERATOR
  contact: hi@example.com                     # DIRECTORY_CONTACT
  include_main: true                          # DIRECTORY_INCLUDE_MAIN
  main_peer_url: https://directory.boson.chat # DIRECTORY_MAIN_PEER ("" = no main directory)
  sync_every: 10m                             # DIRECTORY_SYNC_EVERY (1m–168h)
  peers: [https://dir.example.net]            # replaces DIRECTORY_PEERS
```

The validation rules are the same as for `PATCH /v1/admin/settings`. Every key applies live,
without a restart. `.well-known`, facets, the built-in main peer and the sync interval follow at
once, and `restart_required` is always empty. The directory watches the file's directory
(fsnotify, so an atomic tmp-file + rename write is seen). It also polls the mtime every
`BOSON_CONFIG_POLL` (10 s). Mount the **directory** (`./config:/config:ro`), not the single file:
a bind-mounted file keeps pointing at the old inode after a rename.

**Peers.** Peer state (cursor, sync times, errors, listings) stays in the database. The file only
says which peers must be on:

- Every `directory.peers` URL is created if missing, enabled, and marked
  `managed_by_config_file` (peer JSON). That includes a peer added earlier through the API.
- A file-managed peer is owned by the file. `PATCH` / `DELETE /v1/admin/peers/{id}` answer 409
  `managed_by_config_file`. Sync-now still works.
- A file-managed peer that the file no longer lists is **disabled**, not deleted. Its row and
  state stay, and its listings go, as with any disable. It is then released to the API, so an
  operator may re-enable or delete it. This also happens at start-up for edits made while the
  directory was down.
- Peers that are not in the file stay fully under the peers API, including ones added while the
  file is in use.
- When the file sets `include_main`, the built-in main peer is file-managed as well. It is
  re-enabled when the file says `true`, and the API can't enable or disable it.
- With `peers` absent, `DIRECTORY_PEERS` seeds once as before, and formerly file-managed peers are
  disabled and released. Removing the file entirely leaves peers as they are; the API owns them
  again.

An invalid file (YAML error, bad value, wrong type) is logged with its line and column. It is
reported by `GET /internal/config/status` on the internal listener (`:8081`):
`{path, loaded_at, file_mtime, ok, error?, error_line?, error_column?, restart_required: []}`,
plus `enabled` (false without `BOSON_CONFIG`) and `present`. `file_mtime` is null while the file is
missing. The last good config stays in use. At start-up there is no last good
config: the directory then runs on env + database values (as without the file), the status shows
the error with `loaded_at: null`, and the first valid version is applied live. Reloads that change the section
are audited as `config.reloaded` (actor `config file`, `meta.changed` = keys). Peer changes made
by the file are audited as `peer.add` / `peer.enable` / `peer.disable` by `config file`.

Moving an existing install to the file: `directory export-config` prints the `directory:`
section built from the env, `directory_settings`, and the enabled non-builtin peers. Merge it
into `config/boson.yaml`.

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

Pending [moves](#moving-a-listing-to-a-new-hostname) are checked the same way (strict, within 72 h) by the verify worker.

Workers (verify, health every 15 min with an optional LUSERS read for `user_count`) run in-process;
each run takes a Postgres advisory lock so only one replica runs it.

## Development

```
make test-pg-up test-integration    # unit + Postgres integration tests
make run                            # with .env
```

## License

Copyright (C) 2026 bludot / the boson authors

Licensed under the [GNU Affero General Public License v3.0](LICENSE) only (`AGPL-3.0-only`).

In plain English: if you run a modified version of this software as a network service, you must offer its source code to the people who use that service.
