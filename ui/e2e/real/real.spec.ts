import { expect, request as pwRequest, test, type APIRequestContext } from '@playwright/test';
import { shoot, signIn, strict } from '../helpers';

// Runs against the real directory binary (see README.md in this folder).
const env = (globalThis as { process?: { env: Record<string, string | undefined> } }).process?.env ?? {};
const BASE = env.BASE_URL ?? 'http://localhost:8460';
const PEER = env.PEER_URL ?? 'http://localhost:8470';
const AUTH = env.AUTH_URL ?? 'http://localhost:8790';

const seed: [string, string, string[], string[], string][] = [
	['Libera Lounge', 'lounge', ['community', 'tech'], ['en'], 'A friendly general-purpose network.'],
	['Rust Belt', 'rustbelt', ['programming', 'rust'], ['en'], 'Systems programmers and code reviews.'],
	['Café Crème', 'cafecreme', ['chat', 'social'], ['fr'], 'Discussions en français.'],
	['Nordlys', 'nordlys', ['gaming', 'chat'], ['no', 'sv'], 'Scandinavian gamers and night owls.'],
	['Pixel Forge', 'pixelforge', ['art', 'gamedev'], ['en'], 'Pixel art critiques and jams.'],
	['Retro Terminal', 'retro', ['retro', 'computing'], ['en', 'de'], 'BBS nostalgia and ANSI art.'],
	['Hackerspace', 'hsb', ['hackerspace', 'tech'], ['de', 'en'], 'Der IRC unseres Hackerspaces.'],
	['Kanji Corner', 'kanji', ['language-learning'], ['ja'], 'Learn Japanese together.'],
	['Synthwave', 'synthwave', ['music'], ['en'], 'Neon, arpeggios and modular rigs.'],
	['Linux Help', 'lhd', ['support', 'linux'], ['en'], 'Ask anything about Linux.']
];

async function token(user: string): Promise<string> {
	const ctx = await pwRequest.newContext();
	const r = await ctx.post(AUTH + '/v1/login', { data: { login: user, password: 'password' } });
	expect(r.ok()).toBeTruthy();
	const t = (await r.json()).access_token as string;
	await ctx.dispose();
	return t;
}

/** Registers (idempotent for the same owner) and verifies with the fake DNS verifier. */
async function register(api: APIRequestContext, base: string, bearer: string, n: Record<string, unknown>) {
	const headers = { Authorization: 'Bearer ' + bearer };
	const r = await api.post(base + '/v1/networks', { data: n, headers });
	expect([200, 201]).toContain(r.status());
	const l = await r.json();
	if (l.network.status !== 'verified') {
		const v = await api.post(`${base}/v1/networks/${l.network.id}/verify`, { headers });
		expect([200, 409, 429]).toContain(v.status());
	}
	return l.network.id as string;
}

test.beforeAll(async () => {
	const api = await pwRequest.newContext();
	const demo = await token('demo');
	for (let round = 0; round < 3; round++) {
		for (const [name, slug, tags, languages, description] of seed) {
			await register(api, BASE, demo, {
				hostname: `irc.${slug}${round ? round : ''}.example`,
				port: 6697,
				name: round ? `${name} ${round + 1}` : name,
				description,
				tags,
				languages
			});
		}
	}
	// Listings on the second directory, to import as a peer.
	for (const [name, slug, tags, languages, description] of seed.slice(0, 4)) {
		await register(api, PEER, demo, {
			hostname: `irc.${slug}.friends.example`,
			port: 6697,
			name: `${name} (friends)`,
			description,
			tags,
			languages
		});
	}
	await api.dispose();
});

test('browse pages through the real API', async ({ page }, info) => {
	const ok = strict(page);
	await page.goto('/');
	const cards = page.locator('article.card');
	await expect(cards).toHaveCount(24);
	await expect(page.getByText(/\d+ networks/).first()).toBeVisible();
	await shoot(page, info, 'real-browse');
	await page.locator('.more').scrollIntoViewIfNeeded();
	await expect.poll(() => cards.count()).toBeGreaterThan(24);

	await page.getByRole('combobox', { name: 'Topic' }).click();
	await page.getByRole('option', { name: /^rust/ }).click();
	await expect(page).toHaveURL(/tag=rust/);
	await expect(cards.first()).toContainText('Rust Belt');
	await cards.first().locator('h2 a').click();
	await page.waitForURL(/\/n\/net_/);
	await expect(page.getByRole('link', { name: 'Open in Boson' })).toHaveAttribute(
		'href',
		/^boson:\/\/join\?host=irc\.rustbelt/
	);
	await shoot(page, info, 'real-network');
	ok();
});

test('register, see the TXT record, verify', async ({ page }, info) => {
	const ok = strict(page);
	const host = `irc.web-${info.project.name}-${Date.now().toString(36)}.example`;
	await signIn(page, 'demo', '/me/new');
	await page.getByLabel('Hostname').fill('192.0.2.1');
	await page.getByRole('button', { name: 'Submit and get the DNS record' }).click();
	await expect(page.getByText('IP addresses can’t be listed')).toBeVisible();
	await page.getByLabel('Hostname').fill(host.toUpperCase());
	await page.getByLabel('Network name').fill('Web Test');
	await page.getByLabel('Description').fill('Registered from the web UI.');
	await page.getByRole('button', { name: 'Submit and get the DNS record' }).click();
	await expect(page).toHaveURL(/\/me\/net_/);
	await expect(page.getByLabel('Name', { exact: true })).toHaveValue('_boson.' + host);
	await expect(page.getByLabel('Value', { exact: true })).toHaveValue(/^boson-verify=.+/);
	await expect(page.getByText(/Check it before/)).toBeVisible();
	await shoot(page, info, 'real-register-dns');

	await page.getByRole('button', { name: 'Check DNS now' }).click();
	await expect(page.getByText(`${host} is verified and listed.`)).toBeVisible();
	await expect(page.getByText(/The TXT record at/)).toBeVisible();
	await shoot(page, info, 'real-verified');

	await page.goto('/me');
	await expect(page.locator('.owned li', { hasText: host })).toContainText('Verified');
	ok();
});

test('hostname taken by another account', async ({ page }) => {
	const ok = strict(page);
	await signIn(page, 'operator', '/me/new');
	await page.getByLabel('Hostname').fill('irc.lounge.example');
	await page.getByLabel('Network name').fill('Copycat');
	await page.getByRole('button', { name: 'Submit and get the DNS record' }).click();
	await expect(page.locator('small.err')).toBeVisible();
	await expect(page).toHaveURL(/\/me\/new/);
	ok();
});

test('operator: import a peer, sync, blocks and audit', async ({ page }, info) => {
	const ok = strict(page);
	// Clean slate for the peer so the test can re-run.
	const op = await token('operator');
	const api = await pwRequest.newContext({ extraHTTPHeaders: { Authorization: 'Bearer ' + op } });
	for (const p of (await (await api.get(BASE + '/v1/admin/peers')).json()).peers ?? [])
		if (!p.builtin) await api.delete(`${BASE}/v1/admin/peers/${p.id}`);
	await api.dispose();

	await signIn(page, 'operator', '/operator/peers');
	await expect(page.getByText('This directory doesn’t import the main directory')).toBeVisible();
	await page.getByLabel('Directory address').fill(BASE);
	await page.getByRole('button', { name: 'Check', exact: true }).click();
	await expect(page.getByText('That’s this directory. Add another one.')).toBeVisible();

	await page.getByLabel('Directory address').fill(PEER);
	await page.getByRole('button', { name: 'Check', exact: true }).click();
	await expect(page.getByText('Friends directory').first()).toBeVisible();
	await shoot(page, info, 'real-peer-check');
	await page.getByRole('button', { name: 'Import from Friends directory' }).click();
	const peer = page.locator('.peer', { hasText: 'Friends directory' });
	await expect(peer).toBeVisible();
	await peer.getByRole('button', { name: 'Sync now' }).click();
	await expect(page.getByText('Synced with Friends directory.')).toBeVisible();
	await page.reload();
	await expect(peer).toContainText(/[1-9]\d* listings/);
	await shoot(page, info, 'real-peers');

	await page.getByRole('link', { name: 'Blocks' }).click();
	const pattern = `*.blocked-${info.project.name}.example`;
	await page.getByLabel('Hostname or suffix').fill(pattern);
	await page.getByLabel('Reason').fill('e2e');
	await page.getByRole('button', { name: 'Block', exact: true }).click();
	await expect(page.getByRole('cell', { name: pattern })).toBeVisible();
	await page
		.getByRole('row', { name: new RegExp(pattern.replace(/[.*]/g, '\\$&')) })
		.getByRole('button', { name: 'Unblock' })
		.click();
	await page.getByRole('dialog').getByRole('button', { name: 'Unblock' }).click();
	await expect(page.getByRole('cell', { name: pattern })).toHaveCount(0);

	await page.getByRole('link', { name: 'Listings' }).click();
	await expect(page.locator('tbody tr').first()).toBeVisible();
	await shoot(page, info, 'real-listings');
	await page.getByRole('link', { name: 'Audit log' }).click();
	await expect(page.getByText('Added a peer').first()).toBeVisible();
	await shoot(page, info, 'real-audit');
	ok();
});
