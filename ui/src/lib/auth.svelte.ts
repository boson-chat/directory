// Sign-in with a boson account against the auth service (AUTH_URL).
//
// The directory API wants a bearer access token. auth's cookie mode keeps
// tokens in HttpOnly cookies on its own origin, which a page on another
// origin can't turn into a bearer, so this UI uses the token flow:
//   - password: POST /v1/login {login, password} (no cookie)
//   - SSO: GET /v1/sso/{slug}/start with mode=code + PKCE, back to
//     /signin/callback, then POST /v1/sso/exchange {code, code_verifier}
// The access token (15 min) lives only in memory. The refresh token is kept
// in sessionStorage so a reload in the same tab stays signed in; closing the
// tab ends it. It rotates on every refresh.

import { config } from './config';
import { decodeClaims, type Claims } from './jwt';
import { pkcePair } from './pkce';
import type { AuthConfig, AuthUser, TokenResponse } from './types';

const RT_KEY = 'directory.rt';
const SSO_KEY = 'directory.sso';

export class AuthError extends Error {
	constructor(
		public status: number,
		public code: string,
		message: string
	) {
		super(message);
	}
}

function store(key: string, value: string | null) {
	try {
		if (value === null) sessionStorage.removeItem(key);
		else sessionStorage.setItem(key, value);
	} catch {
		/* storage blocked: the session lasts until reload */
	}
}
function read(key: string): string | null {
	try {
		return sessionStorage.getItem(key);
	} catch {
		return null;
	}
}

async function authCall<T>(path: string, init: { method?: string; body?: unknown; bearer?: string } = {}) {
	const { auth_url } = await config();
	const headers: Record<string, string> = { Accept: 'application/json' };
	if (init.body !== undefined) headers['Content-Type'] = 'application/json';
	if (init.bearer) headers.Authorization = 'Bearer ' + init.bearer;
	let res: Response;
	try {
		res = await fetch(auth_url + path, {
			method: init.method ?? 'GET',
			headers,
			body: init.body === undefined ? undefined : JSON.stringify(init.body),
			credentials: 'omit'
		});
	} catch {
		throw new AuthError(0, 'network', 'The boson sign-in service is unreachable. Check your connection.');
	}
	if (res.status === 204) return undefined as T;
	const data = await res.json().catch(() => null);
	if (!res.ok) {
		const e = (data ?? {}) as { error?: string; message?: string };
		throw new AuthError(res.status, e.error ?? 'http_' + res.status, e.message || res.statusText);
	}
	return data as T;
}

/** Friendly text for an auth failure. */
export function authErrorText(e: unknown): string {
	if (!(e instanceof AuthError)) return e instanceof Error ? e.message : String(e);
	switch (e.code) {
		case 'invalid_credentials':
			return 'That username or password is wrong.';
		case 'email_not_verified':
			return 'Verify your email first: open the link boson sent you, then sign in.';
		case 'account_disabled':
			return 'This account is disabled.';
		case 'rate_limited':
			return 'Too many attempts. Wait a minute and try again.';
		case 'access_denied':
			return 'Sign-in was cancelled.';
		case 'no_linked_account':
			return 'No boson account is linked to that sign-in. Sign in with your password first.';
		case 'invalid_code':
			return 'That sign-in link expired. Start again.';
		default:
			return e.message || 'Sign-in failed. Try again.';
	}
}

class Auth {
	/** In-memory only. */
	#access = '';
	#timer: ReturnType<typeof setTimeout> | undefined;
	#refreshing: Promise<boolean> | null = null;

	claims = $state<Claims | null>(null);
	user = $state<AuthUser | null>(null);
	/** Whether the stored session has been tried on boot. */
	ready = $state(false);

	get signedIn() {
		return this.claims !== null;
	}
	get username() {
		return this.user?.username ?? this.claims?.name ?? '';
	}
	can(perm: string) {
		return this.claims?.perms?.includes(perm) ?? false;
	}
	get token() {
		return this.#access;
	}

	#accept(t: TokenResponse) {
		this.#access = t.access_token;
		this.claims = decodeClaims(t.access_token);
		this.user = t.user ?? null;
		store(RT_KEY, t.refresh_token);
		clearTimeout(this.#timer);
		const ms = Math.max(10, t.expires_in - 60) * 1000;
		this.#timer = setTimeout(() => void this.refresh(), ms);
	}

	#drop() {
		this.#access = '';
		this.claims = null;
		this.user = null;
		store(RT_KEY, null);
		clearTimeout(this.#timer);
	}

	/** Tries the refresh token from this tab's earlier visit. */
	async boot() {
		if (this.ready) return;
		if (read(RT_KEY)) await this.refresh();
		this.ready = true;
	}

	async login(login: string, password: string) {
		this.#accept(await authCall<TokenResponse>('/v1/login', { method: 'POST', body: { login, password } }));
	}

	/** Rotates the refresh token. Concurrent callers share one request. */
	refresh(): Promise<boolean> {
		this.#refreshing ??= (async () => {
			const rt = read(RT_KEY);
			if (!rt) {
				this.#drop();
				return false;
			}
			try {
				this.#accept(
					await authCall<TokenResponse>('/v1/token/refresh', {
						method: 'POST',
						body: { refresh_token: rt }
					})
				);
				return true;
			} catch (e) {
				// A network blip keeps the session for the next try.
				if (!(e instanceof AuthError) || e.status !== 0) this.#drop();
				return false;
			} finally {
				this.#refreshing = null;
			}
		})();
		return this.#refreshing;
	}

	async logout() {
		const bearer = this.#access;
		const rt = read(RT_KEY);
		this.#drop();
		try {
			await authCall('/v1/logout', { method: 'POST', bearer, body: rt ? { refresh_token: rt } : undefined });
		} catch {
			/* the local session is gone either way */
		}
	}

	async providers(): Promise<AuthConfig> {
		return authCall<AuthConfig>('/v1/config');
	}

	/** Sends the browser to an SSO provider; it comes back to /signin/callback. */
	async startSso(startUrl: string, next: string) {
		const { verifier, challenge } = await pkcePair();
		store(SSO_KEY, JSON.stringify({ verifier, next }));
		const { auth_url } = await config();
		const u = new URL(startUrl, auth_url);
		u.searchParams.set('return_to', location.origin + '/signin/callback');
		u.searchParams.set('mode', 'code');
		u.searchParams.set('code_challenge', challenge);
		u.searchParams.set('code_challenge_method', 'S256');
		location.assign(u.toString());
	}

	/** Finishes an SSO sign-in. Returns where to go next. */
	async finishSso(params: URLSearchParams): Promise<string> {
		const saved = read(SSO_KEY);
		store(SSO_KEY, null);
		const { verifier, next } = saved
			? (JSON.parse(saved) as { verifier: string; next: string })
			: ({} as never);
		const err = params.get('error');
		if (err) throw new AuthError(400, err, params.get('error_description') ?? err);
		const code = params.get('code');
		if (!code || !verifier)
			throw new AuthError(400, 'invalid_code', 'This sign-in link is not valid any more.');
		this.#accept(
			await authCall<TokenResponse>('/v1/sso/exchange', {
				method: 'POST',
				body: { code, code_verifier: verifier }
			})
		);
		return safeNext(next);
	}
}

/** Only same-app paths, never //host or absolute URLs. */
export function safeNext(n: string | null | undefined): string {
	return n && n.startsWith('/') && !n.startsWith('//') && !n.startsWith('/\\') ? n : '/me';
}

export const auth = new Auth();
