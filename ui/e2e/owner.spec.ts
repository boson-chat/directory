import { expect, test } from '@playwright/test';
import { reset, shoot, signIn, strict } from './helpers';

test.beforeEach(({ page }) => reset(page));

test('my networks needs a sign-in and lists every state', async ({ page }, info) => {
	const ok = strict(page);
	await page.goto('/me');
	await expect(page).toHaveURL(/\/signin\?next=%2Fme/);
	await shoot(page, info, 'signin');
	await page.getByLabel('Username or email').fill('demo');
	await page.getByLabel('Password').fill('wrong');
	await page.getByRole('button', { name: 'Sign in', exact: true }).click();
	await expect(page.getByText('That username or password is wrong.')).toBeVisible();
	await page.getByLabel('Password').fill('password');
	await page.getByRole('button', { name: 'Sign in', exact: true }).click();
	await expect(page).toHaveURL(/\/me$/);
	await expect(page.locator('.owned li')).toHaveCount(4);
	await expect(page.getByRole('link', { name: 'Operator' })).toHaveCount(0);
	await shoot(page, info, 'me');

	// The session survives a reload in the same tab (refresh token).
	await page.reload();
	await expect(page.locator('.owned li')).toHaveCount(4);
	ok();
});

test('verify shows each resolver, then rate-limits', async ({ page }, info) => {
	const ok = strict(page);
	await signIn(page, 'demo', '/me/net_demo_pending');
	await expect(page.getByRole('heading', { name: 'Prove you run irc.newplace.example' })).toBeVisible();
	await expect(page.getByLabel('Value', { exact: true })).toHaveValue('boson-verify=k7Qm2xVb9pT4wZr8LcN3');
	await expect(page.getByLabel('See what the world sees', { exact: true })).toHaveValue(
		'dig +short TXT _boson.irc.newplace.example'
	);
	await page.getByRole('button', { name: 'Check DNS now' }).click();
	await expect(page.getByText('1 of 3 resolvers see the record.')).toBeVisible();
	await expect(page.locator('.resolvers li')).toHaveCount(3);
	await shoot(page, info, 'verify-partial');
	await page.getByRole('button', { name: 'Check DNS now' }).click();
	await expect(page.getByRole('button', { name: /Check again in \d+s/ })).toBeDisabled();
	ok();
});

test('an expired record gets a new one', async ({ page }, info) => {
	const ok = strict(page);
	await signIn(page, 'demo', '/me/net_demo_expired');
	await expect(page.getByRole('button', { name: 'Check DNS now' })).toBeDisabled();
	await expect(page.getByText(/This record expired/).first()).toBeVisible();
	await shoot(page, info, 'verify-expired');
	await page.getByRole('button', { name: 'New record' }).click();
	await page.getByRole('button', { name: 'Issue new record' }).click();
	await expect(page.getByRole('button', { name: 'Check DNS now' })).toBeEnabled();
	await page.getByRole('button', { name: 'Check DNS now' }).click();
	await expect(page.getByText('irc.expired.example is verified and listed.')).toBeVisible();
	await expect(page.getByText(/The TXT record at/)).toBeVisible();
	ok();
});

test('register validates, then shows the TXT record', async ({ page }, info) => {
	const ok = strict(page);
	await signIn(page, 'demo', '/me/new');
	await page.getByLabel('Hostname').fill('203.0.113.9');
	await page.getByRole('button', { name: 'Submit and get the DNS record' }).click();
	await expect(page.getByText('IP addresses can’t be listed')).toBeVisible();
	await expect(page.getByText('Give the network a name.')).toBeVisible();
	await shoot(page, info, 'register-errors');

	await page.getByLabel('Hostname').fill('irc.libera-lounge.example');
	await page.getByLabel('Hostname').fill('libera-lounge.example');
	await page.getByLabel('Network name').fill('Duplicate');
	await page.getByRole('button', { name: 'Submit and get the DNS record' }).click();
	await expect(page.getByText('already listed')).toBeVisible();

	await page.getByLabel('Hostname').fill('IRC.Fresh-Start.Example');
	await expect(page.getByText('Saved as irc.fresh-start.example.')).toBeVisible();
	await page.getByLabel('Network name').fill('Fresh Start');
	await page.getByLabel('Description').fill('A brand new network.');
	const tags = page.getByRole('textbox', { name: 'Topics' });
	await tags.fill('Retro Gaming');
	await tags.press('Enter');
	await page.getByRole('combobox', { name: 'Languages' }).click();
	await page.getByRole('option', { name: /English/ }).click();
	await page.keyboard.press('Escape');
	await page.getByRole('button', { name: 'Submit and get the DNS record' }).click();
	await expect(page).toHaveURL(/\/me\/net_new/);
	await expect(page.getByLabel('Name', { exact: true })).toHaveValue('_boson.irc.fresh-start.example');
	await shoot(page, info, 'register-dns');
	ok();
});

test('edit, images and typed delete', async ({ page }, info) => {
	const ok = strict(page);
	await signIn(page, 'demo', '/me/net_demo_verified');
	await expect(page.getByText(/was last seen/)).toBeVisible();
	await shoot(page, info, 'manage');
	await page.getByLabel('Network name').fill('The Lounge, renamed');
	await page.getByRole('button', { name: 'Save changes' }).click();
	await expect(page.getByText('Listing saved.')).toBeVisible();
	await expect(page.getByRole('heading', { name: 'The Lounge, renamed' })).toBeVisible();

	// Upload an icon (a 1×1 PNG).
	const png = Buffer.from(
		'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==',
		'base64'
	);
	await page
		.locator('.img.icon input[type=file]')
		.setInputFiles({ name: 'i.png', mimeType: 'image/png', buffer: png });
	await expect(page.getByText('Icon updated.')).toBeVisible();

	await page.getByRole('button', { name: 'Delete listing' }).click();
	const dialog = page.getByRole('dialog');
	await expect(dialog.getByRole('button', { name: 'Delete listing' })).toBeDisabled();
	await dialog.getByRole('textbox').fill('irc.thelounge.example');
	await dialog.getByRole('button', { name: 'Delete listing' }).click();
	await expect(page).toHaveURL(/\/me$/);
	await expect(page.locator('.owned li')).toHaveCount(3);
	ok();
});

test('sign in with SSO comes back signed in', async ({ page }) => {
	const ok = strict(page);
	await page.goto('/signin?next=%2Fme');
	await page.getByRole('button', { name: 'Continue with Example SSO' }).click();
	await expect(page).toHaveURL(/\/me$/);
	await expect(page.locator('.owned li').first()).toBeVisible();
	ok();
});
