import adapter from '@sveltejs/adapter-static';
import { vitePreprocess } from '@sveltejs/vite-plugin-svelte';

/** @type {import('@sveltejs/kit').Config} */
const config = {
	preprocess: vitePreprocess(),
	kit: {
		// Single-page app embedded in the Go binary; the service serves
		// index.html for every client route.
		adapter: adapter({ pages: 'build', assets: 'build', fallback: 'index.html', strict: true }),
		appDir: '_app',
		version: { pollInterval: 0 }
	}
};

export default config;
