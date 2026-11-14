import type { Sort } from './types';

export interface Filters {
	q: string;
	lang: string;
	tag: string;
	source: string;
	sort: Sort;
	nsfw: boolean;
}

const sorts: Sort[] = ['users', 'active', 'newest', 'name'];
export const DEFAULT_FILTERS: Filters = {
	q: '',
	lang: '',
	tag: '',
	source: 'all',
	sort: 'users',
	nsfw: false
};

/** Browse filters from the address bar, so searches can be shared and survive reloads. */
export function readFilters(p: URLSearchParams): Filters {
	const sort = p.get('sort') as Sort;
	return {
		q: (p.get('q') ?? '').slice(0, 200),
		lang: p.get('lang') ?? '',
		tag: p.get('tag') ?? '',
		source: p.get('source') || 'all',
		sort: sorts.includes(sort) ? sort : 'users',
		nsfw: p.get('nsfw') === 'true'
	};
}

/** The query string for f, leaving out defaults. */
export function writeFilters(f: Filters): string {
	const p = new URLSearchParams();
	if (f.q.trim()) p.set('q', f.q.trim());
	if (f.lang) p.set('lang', f.lang);
	if (f.tag) p.set('tag', f.tag);
	if (f.source && f.source !== 'all') p.set('source', f.source);
	if (f.sort !== 'users') p.set('sort', f.sort);
	if (f.nsfw) p.set('nsfw', 'true');
	const s = p.toString();
	return s ? '?' + s : '';
}

export function isFiltered(f: Filters): boolean {
	return writeFilters({ ...f, sort: 'users' }) !== '';
}
