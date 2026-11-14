import { describe, expect, it } from 'vitest';
import { safeNext } from './auth.svelte';
import { decodeClaims } from './jwt';

function jwt(payload: object) {
	const b = (o: object) =>
		btoa(String.fromCharCode(...new TextEncoder().encode(JSON.stringify(o))))
			.replace(/=+$/, '')
			.replace(/\+/g, '-')
			.replace(/\//g, '_');
	return `${b({ alg: 'EdDSA' })}.${b(payload)}.sig`;
}

describe('auth helpers', () => {
	it('decodes token claims', () => {
		const c = decodeClaims(jwt({ sub: 'usr_1', name: 'ünï', perms: ['directory.manage'] }));
		expect(c?.sub).toBe('usr_1');
		expect(c?.name).toBe('ünï');
		expect(c?.perms).toContain('directory.manage');
		expect(decodeClaims('garbage')).toBeNull();
		expect(decodeClaims(jwt({ nosub: 1 }))).toBeNull();
	});
	it('only follows same-app next paths', () => {
		expect(safeNext('/me/net_1')).toBe('/me/net_1');
		expect(safeNext('//evil.example')).toBe('/me');
		expect(safeNext('/\\evil.example')).toBe('/me');
		expect(safeNext('https://evil.example')).toBe('/me');
		expect(safeNext(null)).toBe('/me');
	});
});
