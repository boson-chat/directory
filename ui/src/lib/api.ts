// Typed client for the directory API (/v1, same origin).

import { auth } from './auth.svelte';
import type { BosonTheme, ImageSlot } from './theme';
import type {
	AdminNetworkPage,
	AuditPage,
	Block,
	Facets,
	Listing,
	Network,
	NetworkInput,
	NetworkPage,
	NetworkPatch,
	NetworkStatus,
	Peer,
	PeerCheck,
	DirectorySettings,
	Sort,
	SyncResult,
	VerifyResponse,
	WellKnown
} from './types';

export class ApiError extends Error {
	constructor(
		public status: number,
		public code: string,
		message: string,
		/** The body, for errors that carry data (409 verify reports). */
		public data: unknown = null,
		/** Seconds from Retry-After on 429. */
		public retryAfter = 0
	) {
		super(message);
	}
}

type Method = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE';

interface CallOpts {
	body?: unknown;
	/** Raw bytes (image uploads). */
	blob?: Blob;
	authed?: boolean;
	signal?: AbortSignal;
}

async function call<T>(method: Method, path: string, o: CallOpts = {}, retried = false): Promise<T> {
	const headers: Record<string, string> = { Accept: 'application/json' };
	if (o.body !== undefined) headers['Content-Type'] = 'application/json';
	if (o.blob) headers['Content-Type'] = o.blob.type || 'application/octet-stream';
	if (o.authed && auth.token) headers.Authorization = 'Bearer ' + auth.token;
	let res: Response;
	try {
		res = await fetch(path, {
			method,
			headers,
			body: o.blob ?? (o.body === undefined ? undefined : JSON.stringify(o.body)),
			credentials: 'same-origin',
			signal: o.signal
		});
	} catch (e) {
		if (e instanceof DOMException && e.name === 'AbortError') throw e;
		throw new ApiError(0, 'network', 'The directory is unreachable. Check your connection and try again.');
	}
	// An access token expires every 15 minutes: refresh once and retry.
	if (res.status === 401 && o.authed && !retried && (await auth.refresh())) {
		return call<T>(method, path, o, true);
	}
	if (res.status === 204) return undefined as T;
	let data: unknown = null;
	try {
		data = await res.json();
	} catch {
		/* non-JSON error page from a proxy */
	}
	if (!res.ok) {
		const e = (data ?? {}) as { error?: string; message?: string };
		const retry = Number(res.headers.get('Retry-After')) || 0;
		throw new ApiError(
			res.status,
			e.error ?? 'http_' + res.status,
			e.message || defaultMessage(res.status),
			data,
			retry
		);
	}
	return data as T;
}

function defaultMessage(status: number): string {
	if (status === 401) return 'Sign in again to continue.';
	if (status === 403) return 'Your account can’t do that.';
	if (status === 404) return 'That listing doesn’t exist, or isn’t public.';
	if (status === 429) return 'Too many requests. Wait a moment and try again.';
	if (status >= 500) return 'The directory had a problem. Try again in a minute.';
	return 'The request failed.';
}

function qs(params: Record<string, string | number | boolean | undefined | null>): string {
	const u = new URLSearchParams();
	for (const [k, v] of Object.entries(params)) {
		if (v === undefined || v === null || v === '' || v === false) continue;
		u.set(k, String(v));
	}
	const s = u.toString();
	return s ? '?' + s : '';
}

const id = (s: string) => encodeURIComponent(s);

export interface ListQuery {
	q?: string;
	lang?: string;
	tag?: string;
	nsfw?: boolean;
	source?: string;
	sort?: Sort;
	limit?: number;
	cursor?: string | null;
}

export const directory = {
	// ---- public ----
	list: (p: ListQuery, signal?: AbortSignal) =>
		call<NetworkPage>(
			'GET',
			'/v1/networks' +
				qs({
					...p,
					source: p.source === 'all' ? undefined : p.source,
					nsfw: p.nsfw ? 'true' : undefined
				}),
			{ signal }
		),
	/** Managers get their own pending listing and its record, so send the token when there is one. */
	get: (netId: string) => call<Listing>('GET', `/v1/networks/${id(netId)}`, { authed: true }),
	lookup: (hostname: string) =>
		call<Listing>('GET', '/v1/networks/lookup' + qs({ hostname }), { authed: true }),
	facets: (nsfw = false) => call<Facets>('GET', '/v1/facets' + qs({ nsfw: nsfw ? 'true' : undefined })),
	wellKnown: () => call<WellKnown>('GET', '/.well-known/boson-directory.json'),

	// ---- owner ----
	mine: () => call<{ listings: Listing[] }>('GET', '/v1/me/networks', { authed: true }),
	create: (n: NetworkInput) => call<Listing>('POST', '/v1/networks', { body: n, authed: true }),
	update: (netId: string, p: NetworkPatch) =>
		call<Listing>('PATCH', `/v1/networks/${id(netId)}`, { body: p, authed: true }),
	remove: (netId: string) => call<void>('DELETE', `/v1/networks/${id(netId)}`, { authed: true }),
	verify: (netId: string) =>
		call<VerifyResponse>('POST', `/v1/networks/${id(netId)}/verify`, { authed: true }),
	regenerate: (netId: string) =>
		call<Listing>('POST', `/v1/networks/${id(netId)}/regenerate-token`, { authed: true }),
	putImage: (netId: string, which: 'icon' | 'banner', file: Blob) =>
		call<Listing>('PUT', `/v1/networks/${id(netId)}/${which}`, { blob: file, authed: true }),
	deleteImage: (netId: string, which: 'icon' | 'banner') =>
		call<Listing>('DELETE', `/v1/networks/${id(netId)}/${which}`, { authed: true }),
	/** Image fields omitted or null keep their current image; "" removes one. */
	putTheme: (netId: string, theme: BosonTheme) =>
		call<Listing>('PUT', `/v1/networks/${id(netId)}/theme`, { body: theme, authed: true }),
	deleteTheme: (netId: string) =>
		call<Listing>('DELETE', `/v1/networks/${id(netId)}/theme`, { authed: true }),
	putThemeImage: (netId: string, slot: ImageSlot, file: Blob) =>
		call<Listing>('PUT', `/v1/networks/${id(netId)}/theme-images/${slot}`, { blob: file, authed: true }),
	deleteThemeImage: (netId: string, slot: ImageSlot) =>
		call<Listing>('DELETE', `/v1/networks/${id(netId)}/theme-images/${slot}`, { authed: true }),

	// ---- operator (directory.manage) ----
	peers: () => call<{ peers: Peer[] }>('GET', '/v1/admin/peers', { authed: true }),
	checkPeer: (url: string) => call<PeerCheck>('GET', '/v1/admin/peers/check' + qs({ url }), { authed: true }),
	addPeer: (url: string) => call<{ peer: Peer }>('POST', '/v1/admin/peers', { body: { url }, authed: true }),
	setPeer: (peerId: string, enabled: boolean) =>
		call<{ peer: Peer }>('PATCH', `/v1/admin/peers/${id(peerId)}`, { body: { enabled }, authed: true }),
	syncPeer: (peerId: string) =>
		call<{ peer: Peer; result: SyncResult | null }>('POST', `/v1/admin/peers/${id(peerId)}/sync`, {
			authed: true
		}),
	removePeer: (peerId: string) => call<void>('DELETE', `/v1/admin/peers/${id(peerId)}`, { authed: true }),
	settings: () => call<DirectorySettings>('GET', '/v1/admin/settings', { authed: true }),
	patchSettings: (body: Record<string, unknown>) =>
		call<DirectorySettings>('PATCH', '/v1/admin/settings', { body, authed: true }),

	blocks: () => call<{ blocks: Block[] }>('GET', '/v1/admin/blocks', { authed: true }),
	addBlock: (pattern: string, reason: string) =>
		call<{ block: Block; blocked_network_ids: string[] | null }>('POST', '/v1/admin/blocks', {
			body: { pattern, reason },
			authed: true
		}),
	removeBlock: (blockId: string) => call<void>('DELETE', `/v1/admin/blocks/${id(blockId)}`, { authed: true }),

	listings: (p: { q?: string; status?: NetworkStatus | ''; source?: string; cursor?: string | null }) =>
		call<AdminNetworkPage>('GET', '/v1/admin/networks' + qs(p), { authed: true }),
	moderate: (netId: string, action: 'block' | 'unblock' | 'recheck', reason?: string) =>
		call<Listing>('POST', `/v1/admin/networks/${id(netId)}/${action}`, {
			body: reason ? { reason } : {},
			authed: true
		}),
	adminRemove: (netId: string) => call<void>('DELETE', `/v1/admin/networks/${id(netId)}`, { authed: true }),

	audit: (cursor?: string | null) =>
		call<AuditPage>('GET', '/v1/admin/audit' + qs({ cursor }), { authed: true })
};

export type { Network };
