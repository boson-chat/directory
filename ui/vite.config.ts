import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

// `npm run dev` proxies the API, auth and media to a directory: the mock
// (`npm run mock`, :8787) by default, or DIRECTORY_URL. Run both.
const target =
	(globalThis as { process?: { env: Record<string, string | undefined> } }).process?.env.DIRECTORY_URL ??
	'http://localhost:8787';
const proxied = ['/v1', '/.well-known', '/auth', '/media'];

export default defineConfig({
	plugins: [sveltekit()],
	build: {
		// Never inline fonts as data: URIs; the directory's CSP allows `font-src 'self'` only.
		assetsInlineLimit: (file) => (/\.(woff2?|ttf|otf)$/.test(file) ? false : undefined)
	},
	server: {
		proxy: Object.fromEntries(proxied.map((p) => [p, { target, changeOrigin: false }]))
	}
});
