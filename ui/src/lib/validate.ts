// Client-side checks for the register/edit form. The service re-checks
// everything; these give the error next to the field before a round trip.

export const LIMITS = {
	name: 64,
	description: 500,
	tags: 8,
	tag: 24,
	languages: 5,
	channels: 50
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

/** A channel name a listing may carry: '#', then 1–63 printable ASCII characters, no spaces or commas. */
export const CHANNEL = /^#[\x21-\x2b\x2d-\x7e]{1,63}$/;

/** Trims a typed channel and adds the leading '#'; '' when it can't be a channel name. */
export function normaliseChannel(c: string): string {
	let t = c.trim();
	if (!t) return '';
	if (!t.startsWith('#')) t = '#' + t;
	return CHANNEL.test(t) ? t : '';
}

/** Channels compare ignoring ASCII case (IRC folding), keeping the written case. */
export function channelKey(c: string): string {
	return c.replace(/[A-Z]/g, (x) => x.toLowerCase());
}

/** The first `max` channel names, as the card shows them: "#lobby, #help +2". */
export function channelSummary(list: string[] | undefined, max = 2): string {
	if (!list?.length) return '';
	const head = list.slice(0, max).join(', ');
	return list.length > max ? `${head} +${list.length - max}` : head;
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
	channels?: string;
	autojoin_channels?: string;
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
		channels?: string[];
		autojoin_channels?: string[];
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
	for (const f of ['channels', 'autojoin_channels'] as const) {
		const list = v[f] ?? [];
		if (list.length > LIMITS.channels) e[f] = `Use up to ${LIMITS.channels} channels.`;
		else if (list.some((c) => !CHANNEL.test(c)))
			e[f] = 'Channel names look like #lobby, without spaces or commas.';
	}
	const w = urlError(v.website_url);
	if (w) e.website_url = w;
	return e;
}
