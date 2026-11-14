import { describe, expect, it } from 'vitest';
import {
	cleanTheme,
	contrast,
	contrastIssues,
	listingAccent,
	listingStyle,
	readTheme,
	resolveColors,
	slotValue,
	THEME_TOKENS,
	type BosonTheme
} from './theme';

describe('tokens', () => {
	it('match the client token list', () => {
		expect(THEME_TOKENS.map((t) => t.key)).toEqual([
			'bg',
			'surface',
			'surface-raised',
			'surface-hover',
			'border',
			'border-soft',
			'fg',
			'fg-2',
			'fg-3',
			'muted',
			'meta',
			'accent',
			'accent-on',
			'success',
			'warn',
			'danger',
			'info'
		]);
	});
});

describe('contrast', () => {
	it('is the WCAG ratio', () => {
		expect(contrast('#000000', '#ffffff')).toBeCloseTo(21, 1);
		expect(contrast('#777777', '#777777')).toBe(1);
		expect(contrast('#767676', '#ffffff')).toBeGreaterThan(4.5);
	});
	it('flags weak pairs', () => {
		const t: BosonTheme = { version: 1, base: 'dark', colors: { fg: '#333333', bg: '#222222' } };
		expect(contrastIssues(t).map((i) => i.label)).toContain('Text on the background');
		expect(contrastIssues({ version: 1, base: 'dark' })).toEqual([]);
		expect(contrastIssues({ version: 1, base: 'light' })).toEqual([]);
	});
});

describe('readTheme', () => {
	it('drops invalid fields instead of failing', () => {
		const t = readTheme({
			version: 1,
			base: 'light',
			colors: { accent: '#7C3AED', bg: 'red', nope: '#000000' },
			radius: 40,
			font: 'comic',
			panes: { messages: { image: 'javascript:x', dim: 0.5, blur: 99, position: 'center' }, sidebar: {} },
			logo: 'https://cdn.example/l.svg',
			login_background: 'http://evil.example/x.png'
		});
		expect(t).toEqual({
			version: 1,
			base: 'light',
			colors: { accent: '#7c3aed' },
			panes: { messages: { dim: 0.5, position: 'center' } },
			logo: 'https://cdn.example/l.svg'
		});
	});
	it('rejects other versions and non-objects', () => {
		expect(readTheme({ version: 2 })).toBeNull();
		expect(readTheme(null)).toBeNull();
		expect(readTheme([1])).toBeNull();
	});
});

describe('listingStyle', () => {
	it('applies readable surfaces and accent', () => {
		const s = listingStyle({
			version: 1,
			base: 'dark',
			colors: { bg: '#0b0b10', surface: '#111118', accent: '#7c3aed', 'accent-on': '#ffffff' }
		});
		expect(s).toContain('--surface: #111118');
		expect(s).toContain('--accent: #7c3aed');
		expect(s).toContain('--accent-on: #ffffff');
		// status text follows the dark surface, not the page's light palette
		expect(s).toContain('--success-text: #10b981');
	});
	it('keeps the defaults when text would be under 4.5:1', () => {
		const s = listingStyle({
			version: 1,
			base: 'dark',
			colors: { bg: '#777777', surface: '#777777', fg: '#888888', accent: '#888888', 'accent-on': '#999999' }
		});
		expect(s).not.toContain('--surface');
		expect(s).not.toContain('--fg');
		// accent-on fails, so a readable one is picked instead
		expect(s).toContain('--accent: #888888');
		expect(s).toMatch(/--accent-on: #(ffffff|070709)/);
	});
	it('always finds readable text for an accent (black or white reach 4.58:1)', () => {
		for (const accent of ['#7a7a7a', '#767676', '#ff00ff', '#00ff00'] as const) {
			expect(listingAccent({ version: 1, base: 'dark', colors: { accent } })).toBe(accent);
		}
	});
	it('only an accent: the surfaces stay the directory’s', () => {
		const s = listingStyle({ version: 1, base: 'light', colors: { accent: '#1d4ed8' } });
		expect(s).not.toContain('--surface');
		expect(listingAccent({ version: 1, base: 'light', colors: { accent: '#1d4ed8' } })).toBe('#1d4ed8');
	});
	it('is empty without a theme', () => {
		expect(listingStyle(null)).toBe('');
	});
});

describe('helpers', () => {
	it('fills tokens from the base', () => {
		expect(resolveColors({ base: 'light', colors: { accent: '#123456' } }).bg).toBe('#f4f4f0');
	});
	it('cleans empty panes and reads slots', () => {
		const t = cleanTheme({
			version: 1,
			base: 'dark',
			colors: {},
			panes: { servers: {}, messages: { image: '/m.png' } }
		});
		expect(t).toEqual({ version: 1, base: 'dark', panes: { messages: { image: '/m.png' } } });
		expect(slotValue(t, 'pane-messages')).toBe('/m.png');
		expect(slotValue(t, 'logo')).toBeUndefined();
	});
});
