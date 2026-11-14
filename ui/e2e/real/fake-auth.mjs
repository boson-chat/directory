// A stand-in for the boson auth service, for running the UI against the
// real directory binary: it signs EdDSA access tokens (typ at+jwt, aud
// boson) and publishes them at /.well-known/jwks.json, exactly what the
// directory verifies. Only the endpoints the web UI calls exist.
//
//   PORT=8790 node e2e/real/fake-auth.mjs
//   AUTH_ISSUER=http://localhost:8790 for the directory
//   accounts: demo / password, operator / password (directory.manage)

import { generateKeyPairSync, randomBytes, sign } from 'node:crypto';
import { createServer } from 'node:http';

const PORT = Number(process.env.PORT || 8790);
const ISSUER = process.env.ISSUER || `http://localhost:${PORT}`;
const { publicKey, privateKey } = generateKeyPairSync('ed25519');
const kid = 'key_fake';
const jwk = { ...publicKey.export({ format: 'jwk' }), kid, alg: 'EdDSA', use: 'sig' };

const users = {
	demo: { id: 'usr_01DEMO0000000000000000000', username: 'demo', perms: [] },
	operator: { id: 'usr_01OPERATOR00000000000000', username: 'operator', perms: ['directory.manage'] }
};
const refresh = new Map();

const b64 = (o) => Buffer.from(JSON.stringify(o)).toString('base64url');
function accessToken(u) {
	const now = Math.floor(Date.now() / 1000);
	const head = b64({ alg: 'EdDSA', kid, typ: 'at+jwt' });
	const body = b64({
		iss: ISSUER,
		sub: u.id,
		aud: 'boson',
		sid: 'ses_fake',
		name: u.username,
		role: 'user',
		perms: u.perms,
		iat: now,
		nbf: now,
		exp: now + 900,
		jti: 'jti_' + randomBytes(8).toString('hex')
	});
	const sig = sign(null, Buffer.from(`${head}.${body}`), privateKey).toString('base64url');
	return `${head}.${body}.${sig}`;
}
function tokens(name) {
	const rt = 'bsr_' + randomBytes(16).toString('base64url');
	refresh.set(rt, name);
	const u = users[name];
	return {
		token_type: 'Bearer',
		access_token: accessToken(u),
		expires_in: 900,
		refresh_token: rt,
		refresh_expires_in: 2592000,
		session_id: 'ses_fake',
		user: { id: u.id, username: u.username }
	};
}

async function json(req) {
	const chunks = [];
	for await (const c of req) chunks.push(c);
	try {
		return JSON.parse(Buffer.concat(chunks).toString() || '{}');
	} catch {
		return {};
	}
}

createServer(async (req, res) => {
	const cors = {
		'Access-Control-Allow-Origin': req.headers.origin || '*',
		'Access-Control-Allow-Headers': 'Authorization, Content-Type, X-Requested-With',
		'Access-Control-Allow-Methods': 'GET, POST, OPTIONS',
		Vary: 'Origin'
	};
	const send = (status, body) => {
		res.writeHead(status, { ...cors, 'Content-Type': 'application/json' });
		res.end(body === undefined ? '' : JSON.stringify(body));
	};
	const fail = (status, code, message) => send(status, { error: code, message, code });
	const path = new URL(req.url, ISSUER).pathname;
	if (req.method === 'OPTIONS') return send(204);
	if (path === '/.well-known/jwks.json') return send(200, { keys: [jwk] });
	if (path === '/.well-known/openid-configuration')
		return send(200, { issuer: ISSUER, jwks_uri: ISSUER + '/.well-known/jwks.json' });
	if (path === '/v1/config') return send(200, { registration: 'open', sso_providers: [] });
	if (req.method === 'POST' && path === '/v1/login') {
		const b = await json(req);
		const u = users[String(b.login ?? '').toLowerCase()];
		if (!u || b.password !== 'password')
			return fail(401, 'invalid_credentials', 'invalid username or password');
		return send(200, tokens(u.username));
	}
	if (req.method === 'POST' && path === '/v1/token/refresh') {
		const b = await json(req);
		const who = refresh.get(b.refresh_token);
		if (!who) return fail(401, 'invalid_refresh_token', 'refresh token is not valid');
		refresh.delete(b.refresh_token);
		return send(200, tokens(who));
	}
	if (req.method === 'POST' && path === '/v1/logout') return send(204);
	return fail(404, 'not_found', 'not found');
}).listen(PORT, () => console.log(`fake auth on ${ISSUER}`));
