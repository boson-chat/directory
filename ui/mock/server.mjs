// A tiny in-memory stand-in for the directory API and the auth service, for
// `npm run dev` and the Playwright suite. It serves ui/build (when built)
// under the same strict CSP the directory sends, so CSP breakage shows up.
//
//   node mock/server.mjs           # :8787, or PORT=…
//   accounts: demo / password (owner), operator / password (directory.manage)
//
// Auth is mounted at /auth (the well-known document's `auth` points the app there).

import { createHash, randomBytes } from 'node:crypto';
import { readFileSync, existsSync, statSync } from 'node:fs';
import { createServer } from 'node:http';
import { extname, join, normalize } from 'node:path';
import { fileURLToPath } from 'node:url';
import { audit, bannerSvg, blocks, demoOwned, iconSvg, peers, seedNetworks } from './data.mjs';

const PORT = Number(process.env.PORT || 8787);
const BUILD = fileURLToPath(new URL('../build', import.meta.url));

let state;
function reset() {
	state = {
		networks: [...seedNetworks(), ...demoOwned()],
		peers: structuredClone(peers),
		blocks: structuredClone(blocks),
		audit: structuredClone(audit),
		media: new Map(), // key -> {type, body}
		refresh: new Map(), // refresh token -> username
		lastVerify: new Map(), // network id -> ms
		seq: 1000
	};
	for (const p of state.peers)
		p.listing_count = state.networks.filter((n) => n.origin === 'peer:' + p.id).length;
}
reset();

const users = {
	demo: { id: 'usr_demo', username: 'demo', display_name: 'Demo Operator', perms: [] },
	operator: {
		id: 'usr_op',
		username: 'operator',
		display_name: 'Directory Operator',
		perms: ['directory.manage']
	}
};

// ---------- helpers ----------

const b64url = (s) => Buffer.from(s).toString('base64url');
function accessToken(u) {
	const now = Math.floor(Date.now() / 1000);
	const payload = { sub: u.id, name: u.username, perms: u.perms, iat: now, exp: now + 900, aud: 'boson' };
	return `${b64url(JSON.stringify({ alg: 'none', typ: 'at+jwt' }))}.${b64url(JSON.stringify(payload))}.mock`;
}
function tokens(username) {
	const rt = 'bsr_' + randomBytes(16).toString('base64url');
	state.refresh.set(rt, username);
	const u = users[username];
	return {
		token_type: 'Bearer',
		access_token: accessToken(u),
		expires_in: 900,
		refresh_token: rt,
		refresh_expires_in: 2592000,
		session_id: 'ses_mock',
		user: { id: u.id, username: u.username, display_name: u.display_name }
	};
}
function principal(req) {
	const h = req.headers.authorization || '';
	if (!h.startsWith('Bearer ')) return null;
	try {
		const p = JSON.parse(Buffer.from(h.slice(7).split('.')[1], 'base64url').toString());
		if (p.exp < Date.now() / 1000) return null;
		return Object.values(users).find((u) => u.id === p.sub) ?? null;
	} catch {
		return null;
	}
}

function send(res, status, body, headers = {}) {
	if (body === undefined) {
		res.writeHead(status, headers);
		return res.end();
	}
	res.writeHead(status, { 'Content-Type': 'application/json', 'Cache-Control': 'no-store', ...headers });
	res.end(JSON.stringify(body));
}
const err = (res, status, code, message, headers) =>
	send(res, status, { error: code, message, code }, headers);

async function body(req, raw = false) {
	const chunks = [];
	for await (const c of req) chunks.push(c);
	const buf = Buffer.concat(chunks);
	if (raw) return buf;
	if (!buf.length) return {};
	try {
		return JSON.parse(buf.toString());
	} catch {
		return {};
	}
}

const PUBLIC = [
	'id',
	'hostname',
	'port',
	'tls',
	'name',
	'description',
	'tags',
	'languages',
	'is_nsfw',
	'website_url',
	'auth_url',
	'icon_url',
	'banner_url',
	'status',
	'origin',
	'origin_name',
	'health_status',
	'health_checked_at',
	'user_count',
	'user_count_at',
	'last_verified_at',
	'created_at',
	'updated_at'
];
const pick = (n, keys) => Object.fromEntries(keys.filter((k) => k in n).map((k) => [k, n[k]]));
const TTL = 72 * 3600_000;
const expiresAt = (n) =>
	n.status === 'pending' && n.origin === 'local' && n.token_issued_at
		? new Date(Date.parse(n.token_issued_at) + TTL).toISOString()
		: undefined;
/** The Network JSON; with `manage`, the owner-only fields too (as the directory does). */
function view(n, manage) {
	const v = { ...pick(n, PUBLIC), verification_status: n.status, registered_at: n.created_at };
	if (manage) {
		v.owner = { kind: 'account', auth_user_id: n.owner_auth_user_id };
		if (n.verification_token) {
			v.verification_token = n.verification_token;
			v.token_issued_at = n.token_issued_at;
			const e = expiresAt(n);
			if (e) v.token_expires_at = e;
		}
	}
	return v;
}
/** A Listing: {network, verification} (verification only for managers). */
function listing(n, manage) {
	const verification =
		manage && n.verification_token
			? {
					record_name: '_boson.' + n.hostname,
					record_value: 'boson-verify=' + n.verification_token,
					token: n.verification_token,
					issued_at: n.token_issued_at,
					...(expiresAt(n) ? { expires_at: expiresAt(n) } : {})
				}
			: null;
	return { network: view(n, manage), verification };
}

function audited(action, actor, hostname, meta = null, networkId) {
	state.audit.unshift({
		id: 'aud_' + state.seq++,
		at: new Date().toISOString(),
		action,
		actor: { kind: 'account', id: 'usr_' + actor, name: actor },
		target: hostname ? { network_id: networkId, hostname } : {},
		meta
	});
}

function matches(n, q) {
	if (!q) return true;
	const hay = `${n.name} ${n.hostname} ${n.description ?? ''} ${n.tags.join(' ')}`.toLowerCase();
	return q
		.toLowerCase()
		.split(/\s+/)
		.every((w) => hay.includes(w));
}

const sorts = {
	users: (a, b) => (b.user_count ?? -1) - (a.user_count ?? -1) || a.id.localeCompare(b.id),
	active: (a, b) => b.updated_at.localeCompare(a.updated_at) || a.id.localeCompare(b.id),
	newest: (a, b) => b.created_at.localeCompare(a.created_at) || a.id.localeCompare(b.id),
	name: (a, b) => a.name.localeCompare(b.name) || a.id.localeCompare(b.id)
};

function page(list, url, limitDefault = 25) {
	const limit = Math.min(100, Math.max(1, Number(url.searchParams.get('limit')) || limitDefault));
	const cur = url.searchParams.get('cursor');
	const off = cur ? Number(Buffer.from(cur, 'base64url').toString()) || 0 : 0;
	const slice = list.slice(off, off + limit);
	const next = off + limit < list.length ? Buffer.from(String(off + limit)).toString('base64url') : null;
	return { slice, next };
}

function verifiedPublic(url) {
	const p = url.searchParams;
	const nsfw = p.get('nsfw') === 'true';
	const source = p.get('source');
	return state.networks.filter(
		(n) =>
			n.status === 'verified' &&
			(nsfw || !n.is_nsfw) &&
			(!source ||
				source === 'all' ||
				(source === 'local' ? n.origin === 'local' : n.origin === 'peer:' + source)) &&
			(!p.get('lang') || n.languages.includes(p.get('lang'))) &&
			(!p.get('tag') || n.tags.includes(p.get('tag'))) &&
			matches(n, p.get('q'))
	);
}

const delay = (ms) => new Promise((r) => setTimeout(r, ms));
const DELAY = Number(process.env.MOCK_DELAY ?? 150);

// ---------- auth (/auth) ----------

async function authRoutes(req, res, path, url) {
	const m = req.method;
	if (m === 'GET' && path === '/v1/config') {
		return send(res, 200, {
			registration: 'open',
			require_email_verification: true,
			sso_providers: [
				{
					slug: 'example',
					kind: 'oidc',
					display_name: 'Example SSO',
					start_url: `${url.origin}/auth/v1/sso/example/start`
				}
			]
		});
	}
	if (m === 'POST' && path === '/v1/login') {
		const b = await body(req);
		await delay(DELAY);
		const u = users[String(b.login ?? '').toLowerCase()];
		if (!u || b.password !== 'password')
			return err(res, 401, 'invalid_credentials', 'invalid username or password');
		return send(res, 200, tokens(u.username));
	}
	if (m === 'POST' && path === '/v1/token/refresh') {
		const b = await body(req);
		const who = state.refresh.get(b.refresh_token);
		if (!who) return err(res, 401, 'invalid_refresh_token', 'refresh token is not valid');
		state.refresh.delete(b.refresh_token);
		return send(res, 200, tokens(who));
	}
	if (m === 'POST' && path === '/v1/logout') {
		const b = await body(req);
		state.refresh.delete(b.refresh_token);
		return send(res, 204);
	}
	if (m === 'GET' && path === '/v1/sso/example/start') {
		const rt = url.searchParams.get('return_to');
		const ch = url.searchParams.get('code_challenge');
		const code = 'bsc_' + randomBytes(8).toString('hex');
		state.refresh.set('sso:' + code, JSON.stringify({ ch, user: 'demo' }));
		const to = new URL(rt);
		to.searchParams.set('code', code);
		res.writeHead(302, { Location: to.toString() });
		return res.end();
	}
	if (m === 'POST' && path === '/v1/sso/exchange') {
		const b = await body(req);
		const saved = state.refresh.get('sso:' + b.code);
		state.refresh.delete('sso:' + b.code);
		if (!saved) return err(res, 400, 'invalid_code', 'invalid code');
		const { ch, user } = JSON.parse(saved);
		const got = createHash('sha256')
			.update(b.code_verifier ?? '')
			.digest('base64url');
		if (ch && got !== ch) return err(res, 400, 'invalid_code', 'invalid code');
		return send(res, 200, tokens(user));
	}
	return err(res, 404, 'not_found', 'not found');
}

// ---------- directory (/v1) ----------

async function apiRoutes(req, res, path, url) {
	const m = req.method;
	const me = principal(req);
	await delay(DELAY);

	if (m === 'GET' && path === '/v1/networks') {
		const sort = sorts[url.searchParams.get('sort')] ?? sorts.users;
		const list = verifiedPublic(url).sort(sort);
		const { slice, next } = page(list, url);
		return send(res, 200, {
			networks: slice.map((n) => view(n, false)),
			next_cursor: next,
			total_estimate: list.length
		});
	}
	if (m === 'GET' && path === '/v1/facets') {
		const nsfw = url.searchParams.get('nsfw') === 'true';
		const vis = state.networks.filter((n) => n.status === 'verified' && (nsfw || !n.is_nsfw));
		const count = (f) => {
			const c = new Map();
			for (const n of vis) for (const v of f(n)) c.set(v, (c.get(v) ?? 0) + 1);
			return [...c]
				.map(([value, count]) => ({ value, count }))
				.sort((a, b) => b.count - a.count || a.value.localeCompare(b.value));
		};
		const sources = [
			{ id: 'local', name: 'This directory', count: vis.filter((n) => n.origin === 'local').length },
			...state.peers
				.filter((p) => p.enabled)
				.map((p) => ({
					id: p.id,
					name: p.name,
					url: p.url,
					count: vis.filter((n) => n.origin === 'peer:' + p.id).length
				}))
		];
		return send(res, 200, { tags: count((n) => n.tags), languages: count((n) => n.languages), sources });
	}
	if (m === 'GET' && path === '/v1/me/networks') {
		if (!me) return err(res, 401, 'unauthenticated', 'sign in');
		return send(res, 200, {
			listings: state.networks.filter((n) => n.owner_auth_user_id === me.id).map((n) => listing(n, true))
		});
	}
	if (m === 'POST' && path === '/v1/networks') {
		if (!me) return err(res, 401, 'unauthenticated', 'sign in');
		const b = await body(req);
		const host = String(b.hostname ?? '').toLowerCase();
		if (!host || !b.name) return err(res, 400, 'invalid_request', 'hostname and name are required');
		if (state.networks.some((n) => n.hostname === host))
			return err(res, 409, 'hostname_taken', `${host} is already listed in this directory.`);
		if (
			state.blocks.some((bl) =>
				bl.pattern.startsWith('*.') ? host.endsWith(bl.pattern.slice(1)) : bl.pattern === host
			)
		)
			return err(res, 403, 'blocked', 'This hostname is blocked by the directory operator.');
		const now = new Date().toISOString();
		const n = {
			id: 'net_new' + state.seq++,
			hostname: host,
			port: Number(b.port) || 6697,
			tls: b.tls !== false,
			name: b.name,
			description: b.description ?? '',
			tags: b.tags ?? [],
			languages: b.languages ?? [],
			is_nsfw: !!b.is_nsfw,
			website_url: b.website_url || null,
			auth_url: null,
			icon_url: null,
			banner_url: null,
			status: 'pending',
			origin: 'local',
			health_status: 'unknown',
			user_count: null,
			verification_token: randomBytes(15).toString('base64url'),
			token_issued_at: now,
			last_checked_at: null,
			created_at: now,
			updated_at: now,
			owner_auth_user_id: me.id
		};
		state.networks.push(n);
		audited('network.create', me.username, host, null, n.id);
		return send(res, 201, listing(n, true));
	}

	let mm = path.match(/^\/v1\/networks\/([^/]+)(?:\/(verify|regenerate-token|icon|banner))?$/);
	if (mm) {
		const n = state.networks.find((x) => x.id === decodeURIComponent(mm[1]));
		const own = n && me && (n.owner_auth_user_id === me.id || me.perms.includes('directory.manage'));
		const sub = mm[2];
		if (!n || (!own && n.status !== 'verified' && !sub)) return err(res, 404, 'not_found', 'not found');
		if (m === 'GET' && !sub) return send(res, 200, listing(n, own));
		if (!me) return err(res, 401, 'unauthenticated', 'sign in');
		if (!own) return err(res, 403, 'not_owner', 'not the owner');
		if (n.origin !== 'local' && m !== 'DELETE')
			return err(res, 409, 'not_local', 'imported listings are edited at their own directory');
		if (m === 'PATCH' && !sub) {
			const b = await body(req);
			for (const k of ['name', 'description', 'tags', 'languages', 'is_nsfw', 'website_url'])
				if (k in b) n[k] = b[k];
			n.updated_at = new Date().toISOString();
			return send(res, 200, listing(n, true));
		}
		if (m === 'DELETE' && !sub) {
			state.networks = state.networks.filter((x) => x !== n);
			audited('network.delete', me.username, n.hostname);
			return send(res, 204);
		}
		if (m === 'POST' && sub === 'verify') {
			const last = state.lastVerify.get(n.id) ?? 0;
			if (Date.now() - last < 3000)
				return err(res, 429, 'rate_limited', 'too many verification attempts', { 'Retry-After': '30' });
			state.lastVerify.set(n.id, Date.now());
			if (!n.verification_token)
				return err(res, 409, 'no_token', 'no verification token to check; regenerate first');
			if (n.status === 'pending' && Date.now() - Date.parse(n.token_issued_at) > TTL)
				return err(res, 410, 'token_expired', 'verification token expired; regenerate to try again');
			const rec = [`boson-verify=${n.verification_token}`];
			// irc.newplace.example propagates partially; everything else matches.
			const partial = n.hostname.includes('newplace');
			const results = partial
				? {
						cloudflare: { outcome: 'match', records: rec },
						google: { outcome: 'missing_record', detail: 'no TXT records at _boson.' + n.hostname },
						quad9: { outcome: 'timeout', detail: 'no answer within 3s' }
					}
				: {
						cloudflare: { outcome: 'match', records: rec },
						google: { outcome: 'match', records: rec },
						quad9: { outcome: 'match', records: rec }
					};
			n.last_checked_at = new Date().toISOString();
			if (!partial) {
				n.status = 'verified';
				n.last_verified_at = n.last_checked_at;
			}
			audited('network.verify', me.username, n.hostname, { success: !partial }, n.id);
			const l = listing(n, true);
			return send(res, partial ? 409 : 200, {
				network: l.network,
				server: l.network,
				verification: l.verification,
				success: !partial,
				report: { success: !partial, results }
			});
		}
		if (m === 'POST' && sub === 'regenerate-token') {
			n.verification_token = randomBytes(15).toString('base64url');
			n.token_issued_at = new Date().toISOString();
			if (n.status === 'lapsed') n.status = 'pending';
			return send(res, 200, listing(n, true));
		}
		if (sub === 'icon' || sub === 'banner') {
			if (m === 'PUT') {
				const raw = await body(req, true);
				if (raw.length > 5 * 1024 * 1024)
					return err(res, 413, 'image_too_large', 'images can be up to 5 MiB');
				const type = req.headers['content-type'] || 'application/octet-stream';
				if (!/^image\/(png|jpeg|webp|gif)$/.test(type))
					return err(res, 415, 'unsupported_media_type', 'Use a PNG, JPEG, WebP or GIF.');
				const key = `up-${n.id}-${sub}-${state.seq++}`;
				state.media.set(key, { type, body: raw });
				n[sub + '_url'] = '/media/' + key;
				return send(res, 200, listing(n, true));
			}
			if (m === 'DELETE') {
				n[sub + '_url'] = null;
				return send(res, 200, listing(n, true));
			}
		}
		return err(res, 405, 'method_not_allowed', 'method not allowed');
	}

	// ---------- operator ----------
	if (path.startsWith('/v1/admin/')) {
		if (!me) return err(res, 401, 'unauthenticated', 'sign in');
		if (!me.perms.includes('directory.manage')) return err(res, 403, 'forbidden', 'needs directory.manage');

		if (m === 'GET' && path === '/v1/admin/peers')
			return send(res, 200, {
				peers: state.peers.map((p) => ({ api: p.url + '/v1', active: p.enabled, ...p }))
			});
		if (m === 'GET' && path === '/v1/admin/peers/check') {
			const u = url.searchParams.get('url') ?? '';
			if (!/^https:\/\/[^/]+/.test(u))
				return err(res, 400, 'invalid_url', 'Use the directory’s https:// address.');
			if (u.includes('broken'))
				return err(res, 502, 'peer_unreachable', `${u}/.well-known/boson-directory.json answered 404`);
			const host = new URL(u).host;
			return send(res, 200, {
				url: new URL(u).origin,
				well_known: {
					name: host.split('.')[0] + ' directory',
					api: new URL(u).origin + '/v1',
					version: '1.0.0',
					operator: 'Ops at ' + host,
					contact: 'mailto:ops@' + host,
					federation: true
				}
			});
		}
		if (m === 'POST' && path === '/v1/admin/peers') {
			const b = await body(req);
			const u = new URL(b.url).origin;
			if (u === url.origin) return err(res, 400, 'self_peer', 'a directory can’t peer with itself');
			if (state.peers.some((p) => p.url === u))
				return err(res, 409, 'peer_exists', 'That directory is already a peer.');
			const p = {
				id: 'peer_' + state.seq++,
				url: u,
				name: new URL(u).host.split('.')[0] + ' directory',
				enabled: true,
				builtin: false,
				cursor: 0,
				last_sync_at: null,
				last_error: null,
				listing_count: 0,
				api: u + '/v1',
				active: true,
				created_at: new Date().toISOString()
			};
			state.peers.push(p);
			audited('peer.add', me.username, null, { url: u });
			return send(res, 201, { peer: p });
		}
		mm = path.match(/^\/v1\/admin\/peers\/([^/]+)(\/sync)?$/);
		if (mm) {
			const p = state.peers.find((x) => x.id === mm[1]);
			if (!p) return err(res, 404, 'not_found', 'not found');
			if (m === 'PATCH') {
				const b = await body(req);
				p.enabled = !!b.enabled;
				p.active = p.enabled;
				audited(p.enabled ? 'peer.enable' : 'peer.disable', me.username, null, { peer: p.name });
				return send(res, 200, { peer: p });
			}
			if (m === 'POST' && mm[2]) {
				p.last_sync_at = new Date().toISOString();
				p.last_error = p.enabled ? null : 'peer is disabled';
				return send(res, 200, { peer: p, result: { added: 0, updated: 0, removed: 0 } });
			}
			if (m === 'DELETE') {
				if (p.builtin)
					return err(res, 409, 'builtin_peer', 'The main directory can be switched off, not removed.');
				state.peers = state.peers.filter((x) => x !== p);
				audited('peer.delete', me.username, null, { url: p.url });
				return send(res, 204);
			}
		}
		if (m === 'GET' && path === '/v1/admin/blocks') return send(res, 200, { blocks: state.blocks });
		if (m === 'POST' && path === '/v1/admin/blocks') {
			const b = await body(req);
			if (!b.pattern) return err(res, 400, 'invalid_request', 'pattern is required');
			const bl = {
				id: 'blk_' + state.seq++,
				pattern: String(b.pattern).toLowerCase(),
				reason: b.reason ?? '',
				created_by: me.username,
				created_at: new Date().toISOString()
			};
			state.blocks.unshift(bl);
			audited('block.add', me.username, bl.pattern, { reason: bl.reason });
			return send(res, 201, { block: bl, blocked_network_ids: [] });
		}
		mm = path.match(/^\/v1\/admin\/blocks\/([^/]+)$/);
		if (mm && m === 'DELETE') {
			state.blocks = state.blocks.filter((x) => x.id !== mm[1]);
			return send(res, 204);
		}
		if (m === 'GET' && path === '/v1/admin/networks') {
			const st = url.searchParams.get('status');
			const origin = url.searchParams.get('source') ?? url.searchParams.get('origin');
			const list = state.networks
				.filter(
					(n) =>
						(!st || n.status === st) &&
						(!origin || n.origin === origin) &&
						matches(n, url.searchParams.get('q'))
				)
				.sort(sorts.newest);
			const { slice, next } = page(list, url, 50);
			return send(res, 200, {
				networks: slice.map((n) => view(n, true)),
				next_cursor: next,
				total_estimate: list.length
			});
		}
		mm = path.match(/^\/v1\/admin\/networks\/([^/]+)(?:\/(block|unblock|recheck))?$/);
		if (mm) {
			const n = state.networks.find((x) => x.id === mm[1]);
			if (!n) return err(res, 404, 'not_found', 'not found');
			if (m === 'DELETE') {
				state.networks = state.networks.filter((x) => x !== n);
				audited('network.delete', me.username, n.hostname);
				return send(res, 204);
			}
			if (m === 'POST' && mm[2] === 'block') n.status = 'blocked';
			if (m === 'POST' && mm[2] === 'unblock') n.status = n.last_verified_at ? 'verified' : 'pending';
			if (m === 'POST' && mm[2] === 'recheck') n.last_checked_at = new Date().toISOString();
			audited('network.' + mm[2], me.username, n.hostname, null, n.id);
			return send(res, 200, listing(n, true));
		}
		if (m === 'GET' && path === '/v1/admin/audit') {
			const { slice, next } = page(state.audit, url, 50);
			return send(res, 200, { entries: slice, next_cursor: next });
		}
	}
	return err(res, 404, 'not_found', 'not found');
}

// ---------- static ----------

const types = {
	'.html': 'text/html; charset=utf-8',
	'.js': 'text/javascript',
	'.css': 'text/css',
	'.svg': 'image/svg+xml',
	'.json': 'application/json',
	'.woff2': 'font/woff2',
	'.woff': 'font/woff',
	'.png': 'image/png'
};

let csp = '';
function index() {
	const html = readFileSync(join(BUILD, 'index.html'), 'utf8');
	const hashes = [...html.matchAll(/<script([^>]*)>([\s\S]*?)<\/script>/g)]
		.filter((m) => !m[1].includes('src='))
		.map((m) => `'sha256-${createHash('sha256').update(m[2]).digest('base64')}'`);
	csp = [
		"default-src 'self'",
		`script-src 'self' ${hashes.join(' ')}`,
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data:",
		"font-src 'self'",
		"connect-src 'self'",
		"frame-ancestors 'none'",
		"base-uri 'none'",
		"form-action 'self'",
		"object-src 'none'"
	].join('; ');
	return html;
}

function serveStatic(req, res, path) {
	if (!existsSync(join(BUILD, 'index.html'))) {
		res.writeHead(503, { 'Content-Type': 'text/plain' });
		return res.end('ui/build is missing: run `npm run build`, or use `npm run dev` (which proxies here).');
	}
	const file = normalize(join(BUILD, path));
	if (file.startsWith(BUILD) && path !== '/' && existsSync(file) && statSync(file).isFile()) {
		res.writeHead(200, { 'Content-Type': types[extname(file)] ?? 'application/octet-stream' });
		return res.end(readFileSync(file));
	}
	if (path.startsWith('/_app/') || extname(path)) {
		res.writeHead(404);
		return res.end();
	}
	const html = index();
	res.writeHead(200, {
		'Content-Type': 'text/html; charset=utf-8',
		'Content-Security-Policy': csp,
		'Cache-Control': 'no-cache'
	});
	res.end(html);
}

createServer(async (req, res) => {
	const url = new URL(req.url, `http://${req.headers.host}`);
	const path = url.pathname;
	try {
		// Test hook: back to the seed data.
		if (path === '/__reset' && req.method === 'POST') {
			reset();
			return send(res, 204);
		}
		if (path === '/.well-known/boson-directory.json')
			return send(res, 200, {
				name: 'boson directory',
				api: `${url.origin}/v1`,
				version: 'mock',
				operator: 'boson',
				contact: 'mailto:hello@boson.chat',
				auth: `${url.origin}/auth`,
				federation: true
			});
		if (path.startsWith('/auth/')) return await authRoutes(req, res, path.slice(5), url);
		if (path.startsWith('/v1/')) return await apiRoutes(req, res, path, url);
		let mm = path.match(/^\/media\/(icon|banner)-(\d+)\.svg$/);
		if (mm) {
			const i = Number(mm[2]);
			const n = state.networks.find((x) => x.id === `net_${String(i).padStart(4, '0')}`);
			res.writeHead(200, { 'Content-Type': 'image/svg+xml', 'Cache-Control': 'max-age=3600' });
			return res.end(mm[1] === 'icon' ? iconSvg(i, n?._letter ?? 'B') : bannerSvg(i));
		}
		mm = path.match(/^\/media\/(.+)$/);
		if (mm && state.media.has(mm[1])) {
			const f = state.media.get(mm[1]);
			res.writeHead(200, { 'Content-Type': f.type });
			return res.end(f.body);
		}
		return serveStatic(req, res, path);
	} catch (e) {
		console.error(e);
		return err(res, 500, 'internal', 'mock server error');
	}
}).listen(PORT, () => console.log(`mock directory on http://localhost:${PORT}`));
