import { expect, type Page, type TestInfo } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import { join } from 'node:path';

const env = (globalThis as { process?: { env: Record<string, string | undefined> } }).process?.env ?? {};

/** Puts the mock back to its seed data. */
export async function reset(page: Page) {
	await page.request.post('/__reset');
}

/** Fails the test on any CSP violation or uncaught page error. */
export function strict(page: Page) {
	const problems: string[] = [];
	page.on('console', (m) => {
		if (m.type() === 'error' && /Content Security Policy|Refused to/i.test(m.text())) problems.push(m.text());
	});
	page.on('pageerror', (e) => problems.push(e.message));
	return () => expect(problems, 'CSP violations or page errors').toEqual([]);
}

export async function signIn(page: Page, user = 'demo', next = '/me') {
	await page.goto('/signin?next=' + encodeURIComponent(next));
	await page.getByLabel('Username or email').fill(user);
	await page.getByLabel('Password').fill('password');
	await page.getByRole('button', { name: 'Sign in', exact: true }).click();
	await page.waitForURL((u) => u.pathname === next);
}

/** With SCREENSHOTS=<dir>, saves the page in dark and light for review. */
export async function shoot(page: Page, info: TestInfo, name: string) {
	const dir = env.SCREENSHOTS;
	if (!dir) return;
	mkdirSync(dir, { recursive: true });
	// Bring lazy images in, then back to the top.
	const height = await page.evaluate(() => document.documentElement.scrollHeight);
	const vh = page.viewportSize()?.height ?? 800;
	const width = page.viewportSize()?.width ?? 1280;
	for (let y = 0; y < Math.min(height, 5000); y += vh) {
		await page.evaluate((top) => window.scrollTo(0, top), y);
		await page.waitForTimeout(60);
	}
	await page.evaluate(() => window.scrollTo(0, 0));
	await page.waitForLoadState('networkidle');
	for (const scheme of ['dark', 'light'] as const) {
		await page.emulateMedia({ colorScheme: scheme, reducedMotion: 'reduce' });
		await page.waitForTimeout(120);
		// Long lists are cut at 4000px so the images stay reviewable.
		await page.screenshot({
			path: join(dir, `${name}-${info.project.name}-${scheme}.png`),
			fullPage: true,
			clip: { x: 0, y: 0, width, height: Math.min(height, 4000) }
		});
	}
	await page.emulateMedia({ colorScheme: 'dark' });
}
