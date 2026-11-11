import { expect, test, type Page } from '@playwright/test';

// What every page says when it can't load its data.
const loadError = /Couldn't load|Can't reach Bield/;

/** The text, where it's shown: some pages have a list for phones and a table for wider screens. */
function shown(page: Page, text: string | RegExp, exact = false) {
	return page.getByText(text, { exact }).locator('visible=true').first();
}

/** The link to a device's page, where it's shown. */
function deviceLink(page: Page, name: string) {
	return page
		.locator('a[href^="/devices/"]')
		.filter({ hasText: name })
		.locator('visible=true')
		.first();
}

/** Opens a page and collects what the browser logs as wrong while it settles. */
async function visit(page: Page, path: string) {
	const problems: string[] = [];
	page.on('pageerror', (e) => problems.push(`error: ${e.message}`));
	page.on('console', (m) => {
		if (m.type() === 'error') {
			problems.push(`console: ${m.text()}`);
		}
	});
	await page.goto(path);
	// The live stream is what fills the pages in.
	await expect(page.getByRole('note').filter({ hasText: 'made-up machines' })).toBeVisible();
	return problems;
}

test('the overview shows the demo tailnet', async ({ page }) => {
	const problems = await visit(page, '/');

	await expect(page).toHaveTitle(/Bield demo/);
	await expect(page.getByText(loadError)).toHaveCount(0);
	// Alex's machines, with their state.
	await expect(shown(page, 'Garage Pi', true)).toBeVisible();
	await expect(shown(page, 'NAS', true)).toBeVisible();
	expect(problems).toEqual([]);
});

for (const [path, heading] of [
	['/devices', 'Devices'],
	['/alerts', 'Alerts'],
	['/checks', 'Checks'],
	['/docker', /^\d+ of \d+ running/],
	['/activity', 'Activity'],
	['/devices/coding', 'AI usage'],
	['/settings', 'Settings']
] as const) {
	test(`${path} loads without errors`, async ({ page }) => {
		const problems = await visit(page, path);

		const named = typeof heading === 'string' ? { name: heading, exact: true } : { name: heading };
		await expect(page.getByRole('heading', named).first()).toBeVisible();
		await expect(page.getByText(loadError)).toHaveCount(0);
		expect(problems).toEqual([]);
	});
}

test('the devices page lists the twelve machines', async ({ page }) => {
	await visit(page, '/devices');

	for (const name of ['NAS', 'Homelab', 'Garage Pi', 'MacBook Pro', 'iPhone', "Sam's iPad"]) {
		await expect(shown(page, name, true)).toBeVisible();
	}
});

test('the devices page picks machines by tag', async ({ page }) => {
	const problems = await visit(page, '/devices');
	const main = page.getByRole('main');

	// Only the NAS is tagged storage.
	await expect(shown(page, 'storage', true)).toBeVisible();
	await main.getByLabel('Tag', { exact: true }).selectOption('tag:storage');
	await expect(deviceLink(page, 'NAS')).toBeVisible();
	await expect(deviceLink(page, 'Homelab')).toHaveCount(0);

	expect(problems).toEqual([]);
});

test('the alerts page has what is wrong in the demo', async ({ page }) => {
	await visit(page, '/alerts');

	await expect(shown(page, /Disk \d+% full on NAS/)).toBeVisible();
	await expect(shown(page, 'Garage Pi is offline')).toBeVisible();
	// An admin snoozed the failing check, so it's on the next tab.
	await page.getByRole('tab', { name: /Snoozed/ }).click();
	await expect(shown(page, 'Garage camera is failing')).toBeVisible();
});

test('a device page shows its numbers and what it can do', async ({ page }) => {
	const problems = await visit(page, '/devices');
	await deviceLink(page, 'NAS').click();

	await expect(page).toHaveURL(/\/devices\/[^/]+$/);
	await expect(page.getByRole('heading', { name: 'NAS' }).first()).toBeVisible();
	await expect(page.getByRole('main').getByRole('link', { name: 'Docker' }).first()).toBeVisible();
	await expect(page.getByText(loadError)).toHaveCount(0);
	expect(problems).toEqual([]);
});

test('the AI usage page adds up what the AI tools used, and a device has a card', async ({
	page
}) => {
	const problems = await visit(page, '/devices/coding');

	await expect(shown(page, /API estimate/)).toBeVisible();
	await expect(shown(page, 'Claude Code')).toBeVisible();
	await expect(shown(page, 'Codex')).toBeVisible();
	await page.getByRole('tab', { name: 'Limits' }).click();
	await expect(shown(page, /Codex · weekly limit/)).toBeVisible();
	await expect(page.getByText(loadError)).toHaveCount(0);

	await visit(page, '/devices');
	await deviceLink(page, 'MacBook Pro').click();
	await expect(page.getByRole('heading', { name: 'AI usage' })).toBeVisible();

	// The card is far down the page, so a link under the device's name says it's there and jumps to it.
	const jump = page.locator('a[href="#ai-usage"]');
	await expect(jump).toBeVisible();
	await jump.click();
	await expect(page.getByRole('heading', { name: 'AI usage' })).toBeInViewport();
	expect(problems).toEqual([]);
});

test('the Docker page lists containers, and its actions are off', async ({ page }) => {
	await visit(page, '/devices');
	await deviceLink(page, 'NAS').click();
	await page.getByRole('main').getByRole('link', { name: 'Docker' }).first().click();

	// A wide screen opens it beside the list, and a phone on a page of its own.
	await shown(page, 'jellyfin', true).click();
	await expect(
		page.getByRole('button', { name: /Restart/ }).locator('visible=true')
	).toBeDisabled();
});

for (const [path, what] of [
	['/terminal', 'Terminals'],
	['/files', 'Files']
] as const) {
	test(`${path} says it is turned off`, async ({ page }) => {
		await visit(page, path);

		await expect(page.getByText(`${what} are turned off in this demo`)).toBeVisible();
	});
}
