import { expect, test } from '@playwright/test';
import { reset, shoot, signIn, strict } from './helpers';

test.beforeEach(({ page }) => reset(page));

const logoSvg =
	'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect width="64" height="64" rx="12" fill="#f43f5e"/><circle cx="32" cy="32" r="14" fill="#fff"/></svg>';

test('owners edit the theme with a live preview and upload its images', async ({ page }, info) => {
	const ok = strict(page);
	await signIn(page, 'demo', '/me/net_demo_verified');
	const editor = page.locator('.editor');
	await expect(page.getByRole('heading', { name: 'Theme' })).toBeVisible();
	// The seeded theme shows in the preview (logo on the rail and sign-in).
	await expect(editor.locator('.client .tile.active img')).toHaveAttribute('src', '/media/icon-3.svg');
	await expect(editor.locator('.login .lt')).toHaveText('Sign in to The Lounge');
	await expect(page.getByRole('button', { name: 'Save theme' })).toBeDisabled();
	await shoot(page, info, 'theme-editor');

	// A colour edit is a draft until saved; the preview follows it.
	await editor.getByLabel('Accent colour', { exact: true }).fill('#f43f5e');
	await expect(editor.locator('.client .tile.active')).toHaveCSS('background-color', 'rgb(244, 63, 94)');
	await expect(page.getByRole('button', { name: 'Save theme' })).toBeEnabled();

	// Unreadable text gets a warning.
	await editor.getByLabel('Text colour', { exact: true }).fill('#10211a');
	await expect(editor.locator('.warn')).toContainText('Text on the background');
	await shoot(page, info, 'theme-contrast');
	await editor.getByRole('button', { name: 'Reset Text to the base colour' }).click();
	await expect(editor.locator('.warn')).toHaveCount(0);

	await page.getByRole('button', { name: 'Save theme' }).click();
	await expect(page.getByText('Theme saved.')).toBeVisible();
	await expect(page.getByRole('button', { name: 'Save theme' })).toBeDisabled();

	// Images upload straight to their slot.
	await editor.locator('input[data-slot="logo"]').setInputFiles({
		name: 'logo.svg',
		mimeType: 'image/svg+xml',
		buffer: Buffer.from(logoSvg)
	});
	await expect(page.getByText('Logo updated.')).toBeVisible();
	await expect(editor.locator('.client .tile.active img')).toHaveAttribute(
		'src',
		/theme-net_demo_verified-logo-/
	);
	await editor.locator('input[data-slot="logo"]').setInputFiles({
		name: 'bad.svg',
		mimeType: 'image/svg+xml',
		buffer: Buffer.from('<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>')
	});
	await expect(editor.getByRole('alert')).toContainText('scripts');

	// The pane image controls appear with an image; removing the image hides them.
	await expect(editor.getByText('Dim', { exact: true })).toHaveCount(1);
	await page.getByRole('button', { name: 'Remove messages image' }).click();
	await expect(page.getByText('Messages image removed.')).toBeVisible();
	await expect(editor.getByText('Dim', { exact: true })).toHaveCount(0);

	// The public page now wears the saved accent.
	await page.goto('/n/net_demo_verified');
	await expect(page.locator('article.net')).toHaveAttribute('style', /--accent: #f43f5e/);
	ok();
});

test('the theme can be removed', async ({ page }) => {
	const ok = strict(page);
	await signIn(page, 'demo', '/me/net_demo_verified');
	await page.getByRole('button', { name: 'Remove theme' }).click();
	await page.getByRole('dialog').getByRole('button', { name: 'Remove theme' }).click();
	await expect(page.getByText('Theme removed.')).toBeVisible();
	await expect(page.getByRole('button', { name: 'Remove theme' })).toHaveCount(0);
	await page.goto('/n/net_demo_verified');
	await expect(page.locator('article.net')).not.toHaveAttribute('style', /--accent/);
	ok();
});

test('cards and pages use readable listing colours only', async ({ page }, info) => {
	const ok = strict(page);
	await page.goto('/');
	const card = (name: string) =>
		page.locator('article.card', { has: page.getByRole('link', { name, exact: true }) });
	// Full dark brand: surfaces and accent.
	await expect(card('Libera Lounge')).toHaveAttribute('style', /--surface: #13101f/);
	await expect(card('Libera Lounge')).toHaveCSS('background-color', 'rgb(19, 16, 31)');
	// Text under 4.5:1 on its surfaces: those are dropped, the accent stays.
	const weak = page.locator('article.card').nth(2);
	await expect(weak).toHaveAttribute('style', /--accent: #22d3ee/);
	await expect(weak).not.toHaveAttribute('style', /--surface/);
	await shoot(page, info, 'theme-cards');
	await page.goto('/n/net_0000');
	await expect(page.locator('article.net')).toHaveCSS('background-color', 'rgb(19, 16, 31)');
	await shoot(page, info, 'theme-page');
	ok();
});
