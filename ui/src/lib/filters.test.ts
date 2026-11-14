import { describe, expect, it } from 'vitest';
import { DEFAULT_FILTERS, isFiltered, readFilters, writeFilters } from './filters';

describe('filters', () => {
	it('round-trips through the query string', () => {
		const f = {
			q: 'rust',
			lang: 'en',
			tag: 'tech',
			source: 'peer_main',
			sort: 'newest' as const,
			nsfw: true
		};
		expect(readFilters(new URLSearchParams(writeFilters(f)))).toEqual(f);
	});
	it('leaves defaults out', () => {
		expect(writeFilters(DEFAULT_FILTERS)).toBe('');
		expect(readFilters(new URLSearchParams(''))).toEqual(DEFAULT_FILTERS);
	});
	it('ignores unknown sorts', () => {
		expect(readFilters(new URLSearchParams('sort=evil')).sort).toBe('users');
	});
	it('knows when a search narrows the list', () => {
		expect(isFiltered({ ...DEFAULT_FILTERS, sort: 'name' })).toBe(false);
		expect(isFiltered({ ...DEFAULT_FILTERS, tag: 'x' })).toBe(true);
	});
});
