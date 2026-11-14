import type { Listing, Network } from './types';

/** The client deep link: boson://join?host=&port=&tls=&name= */
export function joinUrl(n: Pick<Network, 'hostname' | 'port' | 'tls' | 'name'>): string {
	const u = new URLSearchParams({
		host: n.hostname,
		port: String(n.port),
		tls: n.tls ? '1' : '0',
		name: n.name
	});
	return 'boson://join?' + u.toString();
}

/** irc.example.org:6697 (TLS) as people type it into other clients. */
export function address(n: Pick<Network, 'hostname' | 'port' | 'tls'>): string {
	return `${n.hostname}:${n.tls ? '+' : ''}${n.port}`;
}

export const recordName = (host: string) => `_boson.${host}`;
export const recordValue = (token: string) => `boson-verify=${token}`;
export const digCommand = (host: string) => `dig +short TXT ${recordName(host)}`;

/** First verification must happen within 72 hours of the token being issued. */
export const TOKEN_TTL_MS = 72 * 3600 * 1000;

/** When the record stops being accepted for the first check (pending local listings). */
export function tokenExpiry(l: Listing): Date | null {
	const n = l.network;
	const at = l.verification?.expires_at ?? n.token_expires_at;
	if (at) return new Date(at);
	const issued = l.verification?.issued_at ?? n.token_issued_at;
	if (issued && n.origin === 'local') return new Date(new Date(issued).getTime() + TOKEN_TTL_MS);
	return null;
}

export function tokenExpired(l: Listing, now = Date.now()): boolean {
	const e = tokenExpiry(l);
	return e !== null && e.getTime() <= now;
}

/** "local" or the peer's label for a listing's origin. */
export function originLabel(n: Pick<Network, 'origin' | 'origin_name'>): string {
	if (n.origin === 'local') return 'local';
	// origin_name can lag behind a just-added peer; never show the raw peer id.
	return n.origin_name || 'another directory';
}

const langNames =
	typeof Intl.DisplayNames === 'function' ? new Intl.DisplayNames(['en'], { type: 'language' }) : null;

/** "en" → "English". */
export function langName(code: string): string {
	try {
		return langNames?.of(code) ?? code;
	} catch {
		return code;
	}
}

/** The common languages offered in the register form. */
export const LANGUAGES = [
	'en',
	'es',
	'pt',
	'fr',
	'de',
	'it',
	'nl',
	'sv',
	'no',
	'da',
	'fi',
	'pl',
	'cs',
	'ru',
	'uk',
	'tr',
	'el',
	'ar',
	'he',
	'fa',
	'hi',
	'bn',
	'ja',
	'ko',
	'zh',
	'vi',
	'th',
	'id',
	'ms',
	'tl',
	'ro',
	'hu'
];
