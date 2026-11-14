// Client-side checks for the register/edit form. The service re-checks
// everything; these give the error next to the field before a round trip.

export const LIMITS = {
	name: 64,
	description: 500,
	tags: 8,
	tag: 24,
	languages: 5
};

const ipv4 = /^\d{1,3}(\.\d{1,3}){3}$/;
const label = /^(?!-)[a-z0-9-]{1,63}(?<!-)$/;

/**
 * Lowercases and IDNA-encodes a hostname the way the directory stores it.
 * Returns the normalised name, or an error message.
 */
export function normaliseHostname(raw: string): { host: string } | { error: string } {
	let h = raw.trim().toLowerCase().replace(/\.$/, '');
	if (!h) return { error: 'Enter the hostname people connect to, like irc.example.org.' };
	if (/^[a-z][a-z0-9+.-]*:\/\//.test(h) || h.includes('/'))
		return { error: 'Enter only the hostname, without ircs:// or a path.' };
	if (h.includes(':') || h.startsWith('['))
		return { error: 'IP addresses can’t be listed; use a hostname you control.' };
	try {
		h = new URL('http://' + h).hostname;
	} catch {
		return { error: 'That isn’t a valid hostname.' };
	}
	if (ipv4.test(h)) return { error: 'IP addresses can’t be listed; use a hostname you control.' };
	const parts = h.split('.');
	if (parts.length < 2) return { error: 'Use a full hostname with a domain, like irc.example.org.' };
	if (h.length > 253 || !parts.every((p) => label.test(p))) return { error: 'That isn’t a valid hostname.' };
	if (/^\d+$/.test(parts[parts.length - 1])) return { error: 'That isn’t a valid hostname.' };
	return { host: h };
}

export function portError(p: number | string): string {
	const n = typeof p === 'number' ? p : Number(p);
	if (!Number.isInteger(n) || n < 1 || n > 65535) return 'Use a port from 1 to 65535.';
	return '';
}

export function normaliseTag(t: string): string {
	return t
		.trim()
		.toLowerCase()
		.replace(/^#/, '')
		.replace(/[\s_]+/g, '-')
		.replace(/[^a-z0-9-]/g, '')
		.replace(/-+/g, '-')
		.replace(/^-|-$/g, '')
		.slice(0, LIMITS.tag);
}

export function urlError(u: string): string {
	if (!u.trim()) return '';
	try {
		const x = new URL(u.trim());
		if (x.protocol !== 'https:' && x.protocol !== 'http:') return 'Use an http:// or https:// address.';
		return '';
	} catch {
		return 'Enter a full address, starting with https://.';
	}
}

export interface FormErrors {
	hostname?: string;
	port?: string;
	name?: string;
	description?: string;
	tags?: string;
	languages?: string;
	website_url?: string;
}

export function validateNetwork(
	v: {
		hostname?: string;
		port?: number | string;
		name: string;
		description: string;
		tags: string[];
		languages: string[];
		website_url: string;
	},
	identity = true
): FormErrors {
	const e: FormErrors = {};
	if (identity) {
		const h = normaliseHostname(v.hostname ?? '');
		if ('error' in h) e.hostname = h.error;
		const p = portError(v.port ?? '');
		if (p) e.port = p;
	}
	const name = v.name.trim();
	if (!name) e.name = 'Give the network a name.';
	else if (name.length > LIMITS.name) e.name = `Keep the name to ${LIMITS.name} characters.`;
	if (v.description.length > LIMITS.description)
		e.description = `Keep the description to ${LIMITS.description} characters.`;
	if (v.tags.length > LIMITS.tags) e.tags = `Use up to ${LIMITS.tags} tags.`;
	if (v.languages.length > LIMITS.languages) e.languages = `Pick up to ${LIMITS.languages} languages.`;
	const w = urlError(v.website_url);
	if (w) e.website_url = w;
	return e;
}
