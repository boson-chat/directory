// boson-theme/1: a network's branding (colours, panes, logo, sign-in
// background), as listings carry it. The token list and the built-in base
// themes are the client's (client/src/renderer/src/modules/ui/theme/
// theme.types.ts and themes.builtin.ts): keep them in step.

export const THEME_TOKENS = [
	{ key: 'bg', label: 'Background', group: 'Surfaces' },
	{ key: 'surface', label: 'Panels', group: 'Surfaces' },
	{ key: 'surface-raised', label: 'Raised panels', group: 'Surfaces' },
	{ key: 'surface-hover', label: 'Hover', group: 'Surfaces' },
	{ key: 'border', label: 'Borders', group: 'Surfaces' },
	{ key: 'border-soft', label: 'Soft borders', group: 'Surfaces' },
	{ key: 'fg', label: 'Text', group: 'Text' },
	{ key: 'fg-2', label: 'Secondary text', group: 'Text' },
	{ key: 'fg-3', label: 'Tertiary text', group: 'Text' },
	{ key: 'muted', label: 'Muted', group: 'Text' },
	{ key: 'meta', label: 'Faint', group: 'Text' },
	{ key: 'accent', label: 'Accent', group: 'Accent' },
	{ key: 'accent-on', label: 'Text on accent', group: 'Accent' },
	{ key: 'success', label: 'Success', group: 'Status' },
	{ key: 'warn', label: 'Warning', group: 'Status' },
	{ key: 'danger', label: 'Danger', group: 'Status' },
	{ key: 'info', label: 'Info', group: 'Status' }
] as const;

export type ThemeToken = (typeof THEME_TOKENS)[number]['key'];
export type ThemeColors = Record<ThemeToken, string>;
export type ThemeBase = 'dark' | 'light';
export type ThemeFont = 'boson' | 'system';
export const PANES = ['servers', 'channels', 'messages', 'members'] as const;
export type PaneName = (typeof PANES)[number];
export const POSITIONS = ['center', 'top', 'bottom', 'left', 'right'] as const;
export type PanePosition = (typeof POSITIONS)[number];
export const MAX_RADIUS = 12;
export const MAX_BLUR = 24;

export interface ThemePane {
	bg?: string;
	image?: string;
	dim?: number;
	blur?: number;
	position?: PanePosition;
}

/** The document as the API returns it (every field optional but version/base). */
export interface BosonTheme {
	version: 1;
	base: ThemeBase;
	colors?: Partial<ThemeColors>;
	nick?: { saturation?: number; lightness?: number };
	radius?: number;
	font?: ThemeFont;
	panes?: Partial<Record<PaneName, ThemePane>>;
	logo?: string;
	logo_wide?: string;
	login_background?: string;
}

/** The image slots of PUT /v1/networks/{id}/theme-images/{slot}. */
export const IMAGE_SLOTS = [
	'logo',
	'logo_wide',
	'login_background',
	'pane-servers',
	'pane-channels',
	'pane-messages',
	'pane-members'
] as const;
export type ImageSlot = (typeof IMAGE_SLOTS)[number];

// Terminal: the client's stock dark look. Daylight: its light built-in.
const TERMINAL: ThemeColors = {
	bg: '#070709',
	surface: '#0d0d11',
	'surface-raised': '#141418',
	'surface-hover': '#1a1a20',
	border: '#24242e',
	'border-soft': '#1c1c24',
	fg: '#e2e2ea',
	'fg-2': '#9898a8',
	'fg-3': '#686878',
	muted: '#444452',
	meta: '#363644',
	accent: '#d97706',
	'accent-on': '#070709',
	success: '#10b981',
	warn: '#f59e0b',
	danger: '#ef4444',
	info: '#3b82f6'
};
const DAYLIGHT: ThemeColors = {
	bg: '#f4f4f0',
	surface: '#fbfbf9',
	'surface-raised': '#ffffff',
	'surface-hover': '#ecece6',
	border: '#d6d6ce',
	'border-soft': '#e4e4dd',
	fg: '#1b1b21',
	'fg-2': '#4e4e5a',
	'fg-3': '#6e6e7a',
	muted: '#9c9ca6',
	meta: '#c2c2c8',
	accent: '#b45309',
	'accent-on': '#ffffff',
	success: '#047857',
	warn: '#b45309',
	danger: '#dc2626',
	info: '#2563eb'
};
const NICK = { dark: { saturation: 62, lightness: 64 }, light: { saturation: 60, lightness: 38 } };

export function baseColors(base: ThemeBase): ThemeColors {
	return base === 'light' ? DAYLIGHT : TERMINAL;
}

/** Every token: the theme's own, else its base's. */
export function resolveColors(t: Pick<BosonTheme, 'base' | 'colors'>): ThemeColors {
	return { ...baseColors(t.base), ...(t.colors ?? {}) } as ThemeColors;
}

export function resolveNick(t: BosonTheme): { saturation: number; lightness: number } {
	return { ...NICK[t.base], ...(t.nick ?? {}) } as { saturation: number; lightness: number };
}

// ---- validation (the contract's rules; the server is authoritative) ----

const HEX = /^#[0-9a-fA-F]{6}$/;
export const isHex = (v: unknown): v is string => typeof v === 'string' && HEX.test(v);
const num = (v: unknown, lo: number, hi: number): number | undefined =>
	typeof v === 'number' && Number.isFinite(v) && v >= lo && v <= hi ? v : undefined;

/** https, or http on loopback. */
export function isImageUrl(v: unknown): v is string {
	if (typeof v !== 'string' || v.length > 2048) return false;
	try {
		const u = new URL(v);
		if (u.username || u.password) return false;
		if (u.protocol === 'https:') return true;
		return u.protocol === 'http:' && ['localhost', '127.0.0.1', '[::1]'].includes(u.hostname);
	} catch {
		return false;
	}
}

/**
 * Reads a theme leniently, the way a client does on read: invalid fields
 * are dropped instead of failing the whole document. Relative image URLs
 * (same origin) are allowed for the mock and local development.
 */
export function readTheme(v: unknown): BosonTheme | null {
	if (!v || typeof v !== 'object' || Array.isArray(v)) return null;
	const o = v as Record<string, unknown>;
	if (o.version !== undefined && o.version !== 1) return null;
	const img = (x: unknown) =>
		isImageUrl(x) || (typeof x === 'string' && x.startsWith('/') && !x.startsWith('//'))
			? (x as string)
			: undefined;
	const t: BosonTheme = { version: 1, base: o.base === 'light' ? 'light' : 'dark' };
	if (o.colors && typeof o.colors === 'object') {
		const c: Partial<ThemeColors> = {};
		for (const { key } of THEME_TOKENS) {
			const val = (o.colors as Record<string, unknown>)[key];
			if (isHex(val)) c[key] = val.toLowerCase();
		}
		if (Object.keys(c).length) t.colors = c;
	}
	if (o.nick && typeof o.nick === 'object') {
		const n = o.nick as Record<string, unknown>;
		const s = num(n.saturation, 0, 100);
		const l = num(n.lightness, 0, 100);
		if (s !== undefined || l !== undefined) t.nick = { saturation: s, lightness: l };
	}
	const r = num(o.radius, 0, MAX_RADIUS);
	if (r !== undefined) t.radius = r;
	if (o.font === 'boson' || o.font === 'system') t.font = o.font;
	if (o.panes && typeof o.panes === 'object') {
		const panes: BosonTheme['panes'] = {};
		for (const name of PANES) {
			const p = (o.panes as Record<string, unknown>)[name];
			if (!p || typeof p !== 'object') continue;
			const src = p as Record<string, unknown>;
			const out: ThemePane = {};
			if (isHex(src.bg)) out.bg = src.bg.toLowerCase();
			const image = img(src.image);
			if (image) out.image = image;
			const dim = num(src.dim, 0, 1);
			if (dim !== undefined) out.dim = dim;
			const blur = num(src.blur, 0, MAX_BLUR);
			if (blur !== undefined) out.blur = blur;
			if (POSITIONS.includes(src.position as PanePosition)) out.position = src.position as PanePosition;
			if (Object.keys(out).length) panes[name] = out;
		}
		if (Object.keys(panes).length) t.panes = panes;
	}
	for (const k of ['logo', 'logo_wide', 'login_background'] as const) {
		const u = img(o[k]);
		if (u) t[k] = u;
	}
	return t;
}

// ---- contrast (WCAG 2) ----

function channel(c: number): number {
	const s = c / 255;
	return s <= 0.03928 ? s / 12.92 : ((s + 0.055) / 1.055) ** 2.4;
}

export function luminance(hex: string): number {
	const n = parseInt(hex.slice(1), 16);
	return 0.2126 * channel((n >> 16) & 255) + 0.7152 * channel((n >> 8) & 255) + 0.0722 * channel(n & 255);
}

export function contrast(a: string, b: string): number {
	const [x, y] = [luminance(a), luminance(b)].sort((p, q) => q - p);
	return (x + 0.05) / (y + 0.05);
}

export const MIN_CONTRAST = 4.5;

export interface ContrastIssue {
	fg: ThemeToken | string;
	bg: ThemeToken | string;
	ratio: number;
	label: string;
}

/** Text/background pairs of t below 4.5:1 (the editor's warning). */
export function contrastIssues(t: BosonTheme): ContrastIssue[] {
	const c = resolveColors(t);
	const pairs: [string, string, string, string, string][] = [
		['fg', 'bg', c.fg, c.bg, 'Text on the background'],
		['fg', 'surface', c.fg, c.surface, 'Text on panels'],
		['fg-2', 'surface', c['fg-2'], c.surface, 'Secondary text on panels'],
		['accent-on', 'accent', c['accent-on'], c.accent, 'Text on accent buttons']
	];
	for (const name of PANES) {
		const bg = t.panes?.[name]?.bg;
		if (bg) pairs.push(['fg', `panes.${name}.bg`, c.fg, bg, `Text on the ${name} pane`]);
	}
	return pairs
		.map(([fg, bg, a, b, label]) => ({ fg, bg, ratio: contrast(a, b), label }))
		.filter((p) => p.ratio < MIN_CONTRAST);
}

// ---- listing cards and pages ----

/**
 * CSS custom properties that paint a listing card or page in the network's
 * colours, guarded: surfaces and text only apply when the theme sets its
 * own surfaces and its text reads at 4.5:1 or better on them; the accent
 * only applies when the text on it does too. Anything that fails keeps the
 * directory's defaults.
 */
export function listingStyle(t: BosonTheme | null | undefined): string {
	if (!t) return '';
	const c = resolveColors(t);
	const own = t.colors ?? {};
	const vars: Record<string, string> = {};
	let ground: string | null = null;
	if (own.bg || own.surface || own['surface-raised']) {
		if (contrast(c.fg, c.surface) >= MIN_CONTRAST && contrast(c.fg, c.bg) >= MIN_CONTRAST) {
			ground = c.surface;
			const quiet = contrast(c['fg-2'], c.surface) >= MIN_CONTRAST ? c['fg-2'] : c.fg;
			Object.assign(vars, {
				'--bg': c.bg,
				'--surface': c.surface,
				'--surface-raised': c['surface-raised'],
				'--surface-hover': c['surface-hover'],
				'--border': c.border,
				'--border-soft': c['border-soft'],
				'--border-strong': `color-mix(in oklab, ${c.border}, ${c.fg} 18%)`,
				'--fg': c.fg,
				'--fg-2': quiet,
				'--fg-3': c['fg-3'],
				'--fg-quiet': quiet,
				'color-scheme': t.base
			});
			// Status colours as text (health, tags): the theme's own where they
			// read on its surface, else plain text.
			for (const k of ['success', 'warn', 'danger', 'info'] as const) {
				vars[`--${k}`] = c[k];
				vars[`--${k}-text`] = contrast(c[k], c.surface) >= MIN_CONTRAST ? c[k] : c.fg;
			}
		}
	}
	if (own.accent) {
		let on = c['accent-on'];
		if (contrast(on, c.accent) < MIN_CONTRAST) {
			// The theme left accent-on to its base: pick whichever reads.
			on = contrast('#ffffff', c.accent) >= contrast('#070709', c.accent) ? '#ffffff' : '#070709';
		}
		if (contrast(on, c.accent) >= MIN_CONTRAST) {
			vars['--accent'] = c.accent;
			vars['--accent-on'] = on;
			vars['--accent-dim'] = `color-mix(in oklab, ${c.accent}, transparent 85%)`;
		}
	}
	// Links and accent text on the theme's own surface: its accent if that
	// reads, else plain text (never the page's palette on a foreign ground).
	if (ground) vars['--accent-text'] = contrast(c.accent, ground) >= MIN_CONTRAST ? c.accent : c.fg;
	return Object.entries(vars)
		.map(([k, v]) => `${k}: ${v}`)
		.join('; ');
}

/** The accent a listing may use (guarded as in listingStyle), or null. */
export function listingAccent(t: BosonTheme | null | undefined): string | null {
	const m = /--accent: (#[0-9a-f]{6})/.exec(listingStyle(t));
	return m ? m[1] : null;
}

// ---- editor helpers ----

/** A copy without empty colour maps and panes, ready to PUT. */
export function cleanTheme(t: BosonTheme): BosonTheme {
	const out: BosonTheme = JSON.parse(JSON.stringify(t));
	if (out.colors && !Object.keys(out.colors).length) delete out.colors;
	if (out.panes) {
		for (const name of PANES) {
			const p = out.panes[name];
			if (p && !Object.values(p).some((v) => v !== undefined && v !== '')) delete out.panes[name];
		}
		if (!Object.keys(out.panes).length) delete out.panes;
	}
	return out;
}

export function slotValue(t: BosonTheme | null | undefined, slot: ImageSlot): string | undefined {
	if (!t) return undefined;
	if (slot.startsWith('pane-')) return t.panes?.[slot.slice(5) as PaneName]?.image;
	return t[slot as 'logo' | 'logo_wide' | 'login_background'];
}

/** Pane background colours a client uses when the theme sets none. */
export function paneDefault(name: PaneName, c: ThemeColors): string {
	return name === 'messages' || name === 'servers' ? c.bg : c.surface;
}
