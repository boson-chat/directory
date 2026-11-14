import { describe, expect, it } from 'vitest';
import { edge, match, place, step, type DropdownOption } from './dropdown';

const opts: DropdownOption[] = [
	{ value: 'a', label: 'Apple', disabled: true },
	{ value: 'b', label: 'Banana' },
	{ value: 'c', label: 'Blueberry' },
	{ value: 'd', label: 'Cherry', disabled: true },
	{ value: 'e', label: 'Date' }
];

describe('step', () => {
	it('skips disabled options and stops at the ends', () => {
		expect(step(opts, 2, 1)).toBe(4);
		expect(step(opts, 4, 1)).toBe(4);
		expect(step(opts, 1, -1)).toBe(1);
		expect(step(opts, 1, 1, 10)).toBe(4);
	});
});

describe('edge', () => {
	it('finds the first and last enabled option', () => {
		expect(edge(opts, 1)).toBe(1);
		expect(edge(opts, -1)).toBe(4);
		expect(edge([{ value: 'x', label: 'X', disabled: true }], 1)).toBe(-1);
	});
});

describe('match', () => {
	it('cycles through options starting with a repeated letter', () => {
		expect(match(opts, 'b', -1)).toBe(1);
		expect(match(opts, 'bb', 1)).toBe(2);
		expect(match(opts, 'bbb', 2)).toBe(1);
	});
	it('matches a longer prefix, keeping the current option if it still fits', () => {
		expect(match(opts, 'bl', 1)).toBe(2);
		expect(match(opts, 'blu', 2)).toBe(2);
	});
	it('ignores disabled options and misses', () => {
		expect(match(opts, 'ch', 0)).toBe(-1);
		expect(match(opts, 'a', 0)).toBe(-1);
		expect(match(opts, '', 0)).toBe(-1);
	});
});

describe('place', () => {
	const vp = { width: 390, height: 800 };
	it('opens below when there is room', () => {
		const p = place({ top: 100, bottom: 138, left: 16, right: 374 }, { width: 200, height: 200 }, vp);
		expect(p).toMatchObject({ up: false, end: false });
		expect(p.maxHeight).toBe(320);
	});
	it('flips up near the bottom of the viewport', () => {
		const p = place({ top: 700, bottom: 738, left: 16, right: 374 }, { width: 200, height: 200 }, vp);
		expect(p.up).toBe(true);
	});
	it('aligns right instead of overflowing, and caps the width', () => {
		const p = place({ top: 100, bottom: 138, left: 250, right: 374 }, { width: 300, height: 100 }, vp);
		expect(p.end).toBe(true);
		expect(p.maxWidth).toBe(366);
		const q = place({ top: 100, bottom: 138, left: 16, right: 100 }, { width: 500, height: 100 }, vp);
		expect(q.end).toBe(false);
		expect(q.maxWidth).toBe(366);
	});
	it('honours a preferred end alignment when it fits', () => {
		const p = place({ top: 0, bottom: 30, left: 250, right: 374 }, { width: 200, height: 100 }, vp, 'end');
		expect(p.end).toBe(true);
	});
});
