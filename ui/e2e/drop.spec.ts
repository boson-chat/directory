import { expect, test, type Locator, type Page } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import { join } from 'node:path';
import { dragOver, dropFiles, fileTransfer, reset, signIn, strict } from './helpers';

const env = (globalThis as { process?: { env: Record<string, string | undefined> } }).process?.env ?? {};

test.beforeEach(({ page }) => reset(page));

// A 1×1 PNG.
const png = {
	name: 'i.png',
	mimeType: 'image/png',
	buffer: Buffer.from(
		'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==',
		'base64'
	)
};
const txt = { name: 'notes.txt', mimeType: 'text/plain', buffer: Buffer.from('hello') };

/** Counts image uploads (PUTs to icon, banner or theme-images). */
function uploads(page: Page) {
	const seen: string[] = [];
	page.on('request', (r) => {
		if (r.method() === 'PUT' && /\/(icon|banner|theme-images\/[a-z_-]+)$/.test(r.url())) seen.push(r.url());
	});
	return seen;
}

/** With SCREENSHOTS=<dir>, saves the field (with a margin) in dark and light. */
async function shootField(page: Page, field: Locator, name: string, project: string) {
	const dir = env.SCREENSHOTS;
	if (!dir) return;
	mkdirSync(dir, { recursive: true });
	await field.scrollIntoViewIfNeeded();
	const box = (await field.boundingBox())!;
	const m = 24;
	for (const scheme of ['dark', 'light'] as const) {
		await page.emulateMedia({ colorScheme: scheme, reducedMotion: 'reduce' });
		await page.waitForTimeout(120);
		await page.screenshot({
			path: join(dir, `${name}-${project}-${scheme}.png`),
			clip: {
				x: Math.max(0, box.x - m),
				y: Math.max(0, box.y - m),
				width: box.width + 2 * m,
				height: box.height + 2 * m
			}
		});
	}
	await page.emulateMedia({ colorScheme: 'dark' });
}

test('an image dropped on the icon field uploads; other files are refused', async ({ page }, info) => {
	const ok = strict(page);
	const sent = uploads(page);
	await signIn(page, 'demo', '/me/net_demo_verified');
	const field = page.locator('.img.icon');
	await expect(field.getByText('or drop an image here')).toBeVisible();

	// Dragging over highlights the whole field; moving onto a child keeps it.
	const dt = await dragOver(field, [png]);
	await expect(field).toHaveClass(/\bover\b/);
	await expect(field.getByText('Drop to upload')).toBeVisible();
	await field.locator('.lbl').dispatchEvent('dragenter', { dataTransfer: dt });
	await field.dispatchEvent('dragleave', { dataTransfer: dt });
	await expect(field).toHaveClass(/\bover\b/);
	await shootField(page, field, 'drop-icon-over', info.project.name);
	await field.locator('.lbl').dispatchEvent('dragleave', { dataTransfer: dt });
	await expect(field).not.toHaveClass(/\bover\b/);

	// A text file is refused inline and never sent.
	await dropFiles(field, [txt]);
	await expect(field.getByRole('alert')).toHaveText("That's not a PNG, JPEG, WebP or GIF image.");
	expect(sent).toEqual([]);

	// An image uploads like a chosen file; of several, the first is used.
	await dropFiles(field, [png, txt]);
	await expect(page.getByText('Icon updated.')).toBeVisible();
	await expect(field.getByRole('alert')).toHaveCount(0);
	await expect(field.locator('img')).toHaveAttribute('src', /\/media\/up-net_demo_verified-icon-/);
	expect(sent).toHaveLength(1);
	ok();
});

test('an image dropped on a theme slot uploads; other files are refused', async ({ page }, info) => {
	const ok = strict(page);
	const sent = uploads(page);
	await signIn(page, 'demo', '/me/net_demo_verified');
	const editor = page.locator('.editor');
	const slot = editor.locator('[data-drop="logo"]');

	await dragOver(slot, [png]);
	await expect(slot).toHaveClass(/\bover\b/);
	await expect(slot.getByText('Drop to upload')).toBeVisible();
	await shootField(page, slot, 'drop-theme-over', info.project.name);

	await slot.dispatchEvent('drop', { dataTransfer: await fileTransfer(page, [txt]) });
	await expect(slot).not.toHaveClass(/\bover\b/);
	await expect(slot.getByRole('alert')).toHaveText("That's not a PNG, JPEG, WebP, GIF or SVG image.");
	expect(sent).toEqual([]);

	await dropFiles(slot, [png]);
	await expect(page.getByText('Logo updated.')).toBeVisible();
	await expect(editor.locator('.client .tile.active img')).toHaveAttribute(
		'src',
		/theme-net_demo_verified-logo-/
	);
	expect(sent).toHaveLength(1);
	ok();
});

test('a file dropped outside an image field does not leave the page', async ({ page }) => {
	await signIn(page, 'demo', '/me/net_demo_verified');
	await expect(page.getByText(/was last seen/)).toBeVisible();
	const dt = await fileTransfer(page, [png]);
	const prevented = await page.evaluate((dataTransfer) => {
		const fire = (type: string) => {
			const e = new DragEvent(type, { dataTransfer, bubbles: true, cancelable: true });
			document.querySelector('main')!.dispatchEvent(e);
			return e.defaultPrevented;
		};
		return { over: fire('dragover'), drop: fire('drop') };
	}, dt);
	expect(prevented).toEqual({ over: true, drop: true });
	await expect(page).toHaveURL(/\/me\/net_demo_verified$/);
});
