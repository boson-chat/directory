import type { UiConfig, WellKnown } from './types';

const fallback: UiConfig = { auth_url: 'https://auth.boson.chat', name: 'boson directory' };
let loaded: Promise<UiConfig> | null = null;

/**
 * Runtime settings from the directory's /.well-known/boson-directory.json:
 * its name and the auth service people sign in with (`auth`, from
 * AUTH_PUBLIC_URL, defaulting to AUTH_ISSUER).
 */
export function config(): Promise<UiConfig> {
	loaded ??= fetch('/.well-known/boson-directory.json', { headers: { Accept: 'application/json' } })
		.then((r): Promise<Partial<WellKnown>> | Partial<WellKnown> => (r.ok ? r.json() : {}))
		.then((w) => ({
			auth_url: trimSlash(w.auth || fallback.auth_url),
			name: w.name || fallback.name
		}))
		.catch(() => fallback);
	return loaded;
}

export function trimSlash(u: string): string {
	return u.replace(/\/+$/, '');
}
