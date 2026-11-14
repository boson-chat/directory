import { defineConfig, devices } from '@playwright/test';

// Two modes:
// - default: the built app against the mock directory (mock/server.mjs),
//   which serves it under the production CSP. `npm run test:e2e` builds first.
// - BASE_URL=http://localhost:8460: e2e/real/*.spec.ts against a running
//   directory binary (see e2e/real/README.md). No mock, no web server.
// SCREENSHOTS=<dir> also saves every page in dark and light.
const env = (globalThis as { process?: { env: Record<string, string | undefined> } }).process?.env ?? {};
const port = Number(env.MOCK_PORT ?? 8788);
const real = !!env.BASE_URL;

export default defineConfig({
	testDir: 'e2e',
	testMatch: real ? 'real/*.spec.ts' : '*.spec.ts',
	testIgnore: real ? [] : ['real/**'],
	timeout: 30_000,
	workers: 1,
	fullyParallel: false,
	use: { baseURL: env.BASE_URL ?? `http://localhost:${port}`, trace: 'retain-on-failure' },
	webServer: real
		? undefined
		: {
				command: `npm run build && PORT=${port} MOCK_DELAY=40 node mock/server.mjs`,
				url: `http://localhost:${port}/.well-known/boson-directory.json`,
				reuseExistingServer: false,
				timeout: 120_000
			},
	projects: [
		{ name: 'desktop', use: { ...devices['Desktop Chrome'], viewport: { width: 1360, height: 900 } } },
		{ name: 'mobile', use: { ...devices['Pixel 7'] } }
	]
});
