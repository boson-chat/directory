import { describe, expect, it } from 'vitest';
import { normaliseHostname, normaliseTag, portError, urlError, validateNetwork } from './validate';

describe('normaliseHostname', () => {
	it('lowercases and drops the trailing dot', () => {
		expect(normaliseHostname(' IRC.Example.ORG. ')).toEqual({ host: 'irc.example.org' });
	});
	it('IDNA-encodes unicode names', () => {
		expect(normaliseHostname('irc.bücher.de')).toEqual({ host: 'irc.xn--bcher-kva.de' });
	});
	it.each([
		'1.2.3.4',
		'::1',
		'[2001:db8::1]',
		'localhost',
		'ircs://irc.example.org',
		'irc.example.org/x',
		'',
		'a..b',
		'-a.example.org'
	])('rejects %s', (h) => {
		expect('error' in normaliseHostname(h)).toBe(true);
	});
});

describe('fields', () => {
	it('checks ports', () => {
		expect(portError(6697)).toBe('');
		expect(portError(0)).not.toBe('');
		expect(portError('70000')).not.toBe('');
		expect(portError('6.5')).not.toBe('');
	});
	it('normalises tags', () => {
		expect(normaliseTag('#Retro Gaming!')).toBe('retro-gaming');
		expect(normaliseTag('--a__b--')).toBe('a-b');
	});
	it('checks website urls', () => {
		expect(urlError('')).toBe('');
		expect(urlError('https://example.org')).toBe('');
		expect(urlError('example.org')).not.toBe('');
		expect(urlError('javascript:alert(1)')).not.toBe('');
	});
	it('collects every error', () => {
		const e = validateNetwork({
			hostname: '10.0.0.1',
			port: 0,
			name: ' ',
			description: 'x'.repeat(501),
			tags: Array(9).fill('a'),
			languages: ['en'],
			website_url: 'nope'
		});
		expect(Object.keys(e).sort()).toEqual(['description', 'hostname', 'name', 'port', 'tags', 'website_url']);
	});
	it('skips identity on edit', () => {
		expect(
			validateNetwork({ name: 'X', description: '', tags: [], languages: [], website_url: '' }, false)
		).toEqual({});
	});
});
