import { expect, test } from '@playwright/test';
import { reset, shoot, signIn, strict } from './helpers';

test.beforeEach(({ page }) => reset(page));

test('operator pages need directory.manage', async ({ page }, info) => {
	await signIn(page, 'demo', '/me');
	await page.goto('/operator/peers');
	await expect(page.getByText('Your account can’t open this page')).toBeVisible();
	await shoot(page, info, 'operator-denied');
});

test('peers: the main directory, add by URL, switch off', async ({ page }, info) => {
	const ok = strict(page);
	await signIn(page, 'operator', '/me');
	const menu = page.getByRole('button', { name: 'Menu' });
	if (await menu.isVisible()) await menu.click();
	await page.getByRole('link', { name: 'Operator' }).first().click();
	await expect(page).toHaveURL(/\/operator\/peers/);
	await expect(page.getByRole('heading', { name: 'The boson main directory' })).toBeVisible();
	await expect(page.getByText('no such host')).toBeVisible();
	await shoot(page, info, 'operator-peers');

	await page.getByLabel('Directory address').fill('dir.friends.example');
	await page.getByRole('button', { name: 'Check', exact: true }).click();
	await expect(page.getByText('https://dir.friends.example/v1')).toBeVisible();
	await shoot(page, info, 'operator-peer-check');
	await page.getByRole('button', { name: /Import from/ }).click();
	await expect(page.locator('.peer')).toHaveCount(4);

	const main = page.locator('.peer').first();
	await main.getByRole('switch').uncheck({ force: true });
	await expect(page.getByText(/Stopped importing from boson directory/)).toBeVisible();
	await expect(main.getByRole('button', { name: 'Sync now' })).toBeDisabled();
	ok();
});

test('blocks, listings and audit', async ({ page }, info) => {
	const ok = strict(page);
	await signIn(page, 'operator', '/operator/blocks');
	await page.getByLabel('Hostname or suffix').fill('bad host');
	await page.getByRole('button', { name: 'Block', exact: true }).click();
	await expect(page.getByText('Use a hostname (irc.example.org)')).toBeVisible();
	await page.getByLabel('Hostname or suffix').fill('*.evil.example');
	await page.getByLabel('Reason').fill('Spam');
	await page.getByRole('button', { name: 'Block', exact: true }).click();
	await expect(page.getByRole('cell', { name: '*.evil.example' })).toBeVisible();
	await shoot(page, info, 'operator-blocks');

	await page.getByRole('link', { name: 'Listings' }).click();
	await expect(page.locator('tbody tr').first()).toBeVisible();
	await page.getByRole('combobox', { name: 'Status' }).click();
	await page.getByRole('option', { name: 'Pending DNS' }).click();
	await expect(page.locator('tbody tr')).toHaveCount(2);
	await shoot(page, info, 'operator-listings');

	await page.getByRole('link', { name: 'Audit log' }).click();
	await expect(page.getByText('Blocked a hostname').first()).toBeVisible();
	await shoot(page, info, 'operator-audit');
	ok();
});
