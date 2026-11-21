import { expect, test } from '@playwright/test';
import { reset, shoot, signIn, strict } from './helpers';

test.beforeEach(({ page }) => reset(page));

test('cards and pages show the channels new users join', async ({ page }, info) => {
	const ok = strict(page);
	await page.goto('/');
	const card = page.locator('article.card', { hasText: 'Libera Lounge' }).first();
	await expect(card.locator('.joins')).toHaveText('Joins #lobby');
	await expect(page.locator('article.card .joins', { hasText: '#welcome, #games +1' })).toHaveCount(1);
	await shoot(page, info, 'channels-browse');

	await card.locator('h2 a').click();
	const chans = page.getByRole('region', { name: 'Channels' });
	await expect(chans).toContainText('Joins #lobby when you first connect.');
	await expect(chans.getByRole('listitem')).toHaveText(['#lobby', '#help', '#dev', '#offtopic']);
	await shoot(page, info, 'channels-page');

	// A listing without channels shows neither.
	await page.goto('/n/net_0003');
	await expect(page.getByRole('heading', { level: 1 })).toBeVisible();
	await expect(page.getByRole('region', { name: 'Channels' })).toHaveCount(0);
	ok();
});

test('the owner edits channels and auto-join channels', async ({ page }, info) => {
	const ok = strict(page);
	await signIn(page, 'demo', '/me/net_demo_verified');
	const channels = page.getByRole('textbox', { name: 'Channels' });
	const autojoin = page.getByRole('textbox', { name: 'New users join' });
	await expect(page.getByRole('button', { name: 'Remove #help' })).toBeVisible();

	// '#' is added, case-insensitive duplicates and bad names are refused.
	await channels.fill('dev');
	await channels.press('Enter');
	await channels.fill('#DEV');
	await channels.press('Enter');
	await channels.fill('#bad name');
	await channels.press('Enter');
	await expect(channels).toHaveValue('#bad name');
	await channels.fill('');
	await autojoin.fill('#Dev');
	await autojoin.press('Enter');
	await page.getByRole('button', { name: 'Remove #help' }).click();
	await shoot(page, info, 'channels-form');

	const saved = page.waitForResponse((r) => r.request().method() === 'PATCH');
	await page.getByRole('button', { name: 'Save changes' }).click();
	const body = (await saved).request().postDataJSON();
	expect(body.channels).toEqual(['#lobby', '#dev']);
	expect(body.autojoin_channels).toEqual(['#lobby', '#Dev']);
	await expect(page.getByText('Listing saved.')).toBeVisible();

	await page.goto('/n/net_demo_verified');
	await expect(page.getByRole('region', { name: 'Channels' })).toContainText('Joins #lobby, #Dev');
	ok();
});
