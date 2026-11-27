import { expect, test } from '@playwright/test';
import { reset, shoot, shootElement, signIn, strict } from './helpers';

test.beforeEach(({ page }) => reset(page));

test('move a listing to a new hostname: pending, check, cancel, move', async ({ page }, info) => {
	const ok = strict(page);
	await signIn(page, 'demo', '/me/net_demo_verified');
	const sec = page.locator('section', { has: page.getByRole('heading', { name: 'Move to a new hostname' }) });
	await expect(sec).toBeVisible();
	await shoot(page, info, 'move-start');
	await shootElement(sec, info, 'move-start-section');

	// Validation and conflicts show next to the field.
	await sec.getByRole('button', { name: 'Start move' }).click();
	await expect(sec.getByText('Enter the hostname people connect to')).toBeVisible();
	await sec.getByLabel('New hostname').fill('irc.newplace.example');
	await sec.getByRole('button', { name: 'Start move' }).click();
	await expect(sec.getByText('already listed by someone else')).toBeVisible();

	// Pending: the record for the new hostname; the listing keeps its address.
	await sec.getByLabel('New hostname').fill('IRC.Boson.NewPlace-Lounge.example');
	await expect(sec.getByText('Saved as irc.boson.newplace-lounge.example.')).toBeVisible();
	await sec.getByRole('button', { name: 'Start move' }).click();
	const pending = page.locator('section', {
		has: page.getByRole('heading', { name: 'Moving to irc.boson.newplace-lounge.example' })
	});
	await expect(pending).toBeVisible();
	await expect(pending.getByLabel('Name', { exact: true })).toHaveValue(
		'_boson.irc.boson.newplace-lounge.example'
	);
	await expect(pending.getByLabel('Value', { exact: true })).toHaveValue(/^boson-verify=.+/);
	await expect(page.locator('.head .addr')).toHaveText('irc.thelounge.example:+6697');
	await pending.getByRole('button', { name: 'Check DNS now' }).click();
	await expect(pending.getByText('1 of 3 resolvers see the record.')).toBeVisible();
	await expect(page.locator('.head .addr')).toHaveText('irc.thelounge.example:+6697');
	await shootElement(pending, info, 'move-pending-section');
	await shootElement(pending.locator('.steps > li').first(), info, 'move-pending-route');
	// No sideways scrolling at phone width.
	expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);

	// Cancel.
	await pending.getByRole('button', { name: 'Cancel move' }).click();
	await page.getByRole('dialog').getByRole('button', { name: 'Cancel move' }).click();
	await expect(sec.getByRole('button', { name: 'Start move' })).toBeVisible();

	// A move that verifies: same listing, new address, the old one remembered.
	await sec.getByLabel('New hostname').fill('irc.boson.thelounge.example');
	await sec.getByRole('button', { name: 'Start move' }).click();
	const moving = page.locator('section', {
		has: page.getByRole('heading', { name: 'Moving to irc.boson.thelounge.example' })
	});
	// (Checks are rate limited per listing: the mock's window is 3 s.)
	await page.waitForTimeout(3100);
	await moving.getByRole('button', { name: 'Check DNS now' }).click();
	await expect(page.getByText('The Lounge moved to irc.boson.thelounge.example.')).toBeVisible();
	await expect(page.locator('.head .addr')).toHaveText('irc.boson.thelounge.example:+6697');
	await expect(sec.getByText('Previously listed as')).toContainText('irc.thelounge.example');
	await expect(page).toHaveURL(/\/me\/net_demo_verified$/);
	await shoot(page, info, 'move-done');
	await shootElement(sec, info, 'move-done-section');
	ok();
});
