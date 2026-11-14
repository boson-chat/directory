import { describe, expect, it } from 'vitest';
import { address, digCommand, joinUrl, originLabel, recordValue, tokenExpired, tokenExpiry } from './links';
import type { Listing } from './types';

describe('links', () => {
	const n = { hostname: 'irc.example.org', port: 6697, tls: true, name: 'Example & Co' };
	it('builds the boson:// join link', () => {
		const u = new URL(joinUrl(n));
		expect(u.protocol).toBe('boson:');
		expect(u.searchParams.get('host')).toBe('irc.example.org');
		expect(u.searchParams.get('port')).toBe('6697');
		expect(u.searchParams.get('tls')).toBe('1');
		expect(u.searchParams.get('name')).toBe('Example & Co');
	});
	it('formats addresses and records', () => {
		expect(address(n)).toBe('irc.example.org:+6697');
		expect(address({ ...n, tls: false, port: 6667 })).toBe('irc.example.org:6667');
		expect(recordValue('abc')).toBe('boson-verify=abc');
		expect(digCommand('irc.example.org')).toBe('dig +short TXT _boson.irc.example.org');
	});
	it('computes token expiry from issue time', () => {
		const issued = '2026-01-01T00:00:00Z';
		const o = {
			network: { origin: 'local', token_issued_at: issued },
			verification: null
		} as unknown as Listing;
		expect(tokenExpiry(o)?.toISOString()).toBe('2026-01-04T00:00:00.000Z');
		expect(tokenExpired(o, Date.parse('2026-01-03T23:00:00Z'))).toBe(false);
		expect(tokenExpired(o, Date.parse('2026-01-04T00:00:01Z'))).toBe(true);
		// The server's expires_at wins; imported listings have none.
		const v = {
			network: { origin: 'local' },
			verification: { expires_at: '2026-02-01T00:00:00Z' }
		} as unknown as Listing;
		expect(tokenExpiry(v)?.toISOString()).toBe('2026-02-01T00:00:00.000Z');
		expect(
			tokenExpiry({
				network: { origin: 'peer:x', token_issued_at: issued },
				verification: null
			} as unknown as Listing)
		).toBeNull();
	});
	it('labels origins', () => {
		expect(originLabel({ origin: 'local' })).toBe('local');
		expect(originLabel({ origin: 'peer:abc', origin_name: 'Friends' })).toBe('Friends');
		expect(originLabel({ origin: 'peer:abc' })).toBe('another directory');
	});
});
