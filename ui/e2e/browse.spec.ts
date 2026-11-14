import { expect, test } from '@playwright/test';
import { reset, shoot, strict } from './helpers';

test.beforeEach(({ page }) => reset(page));

test('browse pages through every network with infinite scroll', async ({ page }, info) => {
	const ok = strict(page);
	await page.goto('/');
	await expect(page.getByRole('heading', { name: 'Find a network to join' })).toBeVisible();
	const cards = page.locator('article.card');
	await expect(cards).toHaveCount(24);
	await expect(page.getByText(/\d+ networks from 3 directories/)).toBeVisible();
	await shoot(page, info, 'browse');

	// Scrolling to the end fetches the next pages (cursor pagination).
	for (let i = 0; i < 4; i++) {
		await page.locator('.more').scrollIntoViewIfNeeded();
		await page.waitForTimeout(300);
	}
	await expect(page.getByText('That’s every network.')).toBeVisible();
	expect(await cards.count()).toBeGreaterThan(48);
	ok();
});

test('filters live in the address bar', async ({ page }, info) => {
	const ok = strict(page);
	await page.goto('/');
	await page.getByRole('button', { name: 'chat', exact: true }).first().click();
	await expect(page).toHaveURL(/tag=chat/);
	await expect(page.locator('article.card').first()).toBeVisible();
	for (const t of await page.locator('article.card .tags').allInnerTexts()) expect(t).toContain('chat');

	await page.getByRole('searchbox', { name: 'Search networks' }).fill('zzzz-nothing');
	await expect(page.getByText('No networks match')).toBeVisible();
	await shoot(page, info, 'browse-empty');
	await page.getByRole('button', { name: 'Clear filters' }).first().click();
	await expect(page).toHaveURL(/\/$/);

	// Source filter: only listings imported from a peer.
	await page.getByRole('combobox', { name: 'Source' }).click();
	await page.getByRole('option', { name: /Annuaire IRC-FR/ }).click();
	await expect(page).toHaveURL(/source=peer_fr/);
	await expect(page.locator('article.card').first()).toContainText('via Annuaire IRC-FR');

	// 18+ listings are hidden until asked for.
	await page.goto('/?q=adults');
	await expect(page.getByText('No networks match')).toBeVisible();
	await page.getByText('Show 18+').click();
	await expect(page).toHaveURL(/nsfw=true/);
	await expect(page.locator('article.card .tag-bad').first()).toHaveText('18+');
	ok();
});

test('a network page links into the app', async ({ page }, info) => {
	const ok = strict(page);
	await page.goto('/');
	await page.locator('article.card h2 a').first().click();
	await expect(page).toHaveURL(/\/n\/net_/);
	const open = page.getByRole('link', { name: 'Open in Boson' });
	await expect(open).toHaveAttribute('href', /^boson:\/\/join\?host=[^&]+&port=\d+&tls=[01]&name=/);
	await expect(page.getByText('Connect from another client')).toBeVisible();
	await shoot(page, info, 'network');

	await page.goto('/n/net_missing');
	await expect(page.getByText('This network isn’t listed')).toBeVisible();
	await shoot(page, info, 'network-missing');
	ok();
});

test('the directory being down reads as an error, not an empty list', async ({ page }, info) => {
	await page.route('**/v1/networks?*', (r) =>
		r.fulfill({ status: 503, body: '{"error":"unavailable","message":"The directory is restarting."}' })
	);
	await page.goto('/');
	await expect(page.getByText('The directory didn’t answer')).toBeVisible();
	await expect(page.getByText('The directory is restarting.')).toBeVisible();
	await shoot(page, info, 'browse-error');
});
