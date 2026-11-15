import type { ThemeBase } from './theme';

/**
 * The page's own colour scheme: the site's theme picker (data-theme on
 * <html>) when set, else the device's prefers-color-scheme. Dark is the
 * site's default. Listings use the matching variant of their theme.
 */
class Scheme {
	current = $state<ThemeBase>('dark');

	constructor() {
		if (typeof window === 'undefined' || typeof document === 'undefined') return;
		const root = document.documentElement;
		const media = window.matchMedia?.('(prefers-color-scheme: light)');
		const read = () => {
			const picked = root.dataset.theme;
			this.current = picked === 'dark' || picked === 'light' ? picked : media?.matches ? 'light' : 'dark';
		};
		read();
		media?.addEventListener?.('change', read);
		new MutationObserver(read).observe(root, { attributes: true, attributeFilter: ['data-theme'] });
	}
}

export const scheme = new Scheme();
