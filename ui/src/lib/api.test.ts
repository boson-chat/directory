import { afterEach, describe, expect, it, vi } from 'vitest';
import { ApiError, directory } from './api';

function respond(status: number, body: unknown, headers: Record<string, string> = {}) {
	return new Response(body === undefined ? null : JSON.stringify(body), {
		status,
		headers: { 'Content-Type': 'application/json', ...headers }
	});
}

afterEach(() => vi.unstubAllGlobals());

describe('directory client', () => {
	it('builds the list query, leaving defaults out', async () => {
		const f = vi.fn().mockResolvedValue(respond(200, { networks: [], next_cursor: null, total_estimate: 0 }));
		vi.stubGlobal('fetch', f);
		await directory.list({ q: 'rust', source: 'all', nsfw: false, sort: 'name', limit: 24, cursor: 'abc' });
		expect(f.mock.calls[0][0]).toBe('/v1/networks?q=rust&sort=name&limit=24&cursor=abc');
		await directory.list({ nsfw: true, source: 'peer_1' });
		expect(f.mock.calls[1][0]).toBe('/v1/networks?nsfw=true&source=peer_1');
	});

	it('keeps the verify report on 409', async () => {
		const body = { success: false, report: { success: false, results: { google: { outcome: 'timeout' } } } };
		vi.stubGlobal('fetch', vi.fn().mockResolvedValue(respond(409, body)));
		const e = await directory.verify('net_1').catch((x) => x);
		expect(e).toBeInstanceOf(ApiError);
		expect(e.status).toBe(409);
		expect(e.data).toEqual(body);
	});

	it('reads Retry-After on 429', async () => {
		vi.stubGlobal(
			'fetch',
			vi
				.fn()
				.mockResolvedValue(
					respond(429, { error: 'rate_limited', message: 'slow down' }, { 'Retry-After': '17' })
				)
		);
		const e = await directory.verify('net_1').catch((x) => x);
		expect(e.code).toBe('rate_limited');
		expect(e.retryAfter).toBe(17);
	});

	it('uploads images as raw bytes with their type', async () => {
		const f = vi.fn().mockResolvedValue(respond(200, {}));
		vi.stubGlobal('fetch', f);
		await directory.putImage('net_1', 'banner', new Blob(['x'], { type: 'image/png' }));
		const [url, init] = f.mock.calls[0];
		expect(url).toBe('/v1/networks/net_1/banner');
		expect(init.method).toBe('PUT');
		expect(init.headers['Content-Type']).toBe('image/png');
	});

	it('turns a network failure into a readable error', async () => {
		vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new TypeError('Failed to fetch')));
		const e = await directory.facets().catch((x) => x);
		expect(e.code).toBe('network');
		expect(e.message).toMatch(/unreachable/);
	});
});
