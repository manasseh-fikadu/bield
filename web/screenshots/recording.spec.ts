import { test, type Locator, type Page } from '@playwright/test';
import { link, open } from './shoot';

// The clip for the README: about twenty seconds of the demo, with a cursor, since the video
// doesn't record one. `make recording` turns the video this saves into docs/images/demo.webp.

const video = process.env.BIELD_RECORDING ?? 'test-results/recording.webm';

/** A dot that follows the mouse, and rings where it clicks. */
async function addCursor(page: Page) {
	await page.addInitScript(() => {
		const draw = () => {
			const dot = document.createElement('div');
			dot.style.cssText =
				'position:fixed;left:0;top:0;width:18px;height:18px;margin:-9px 0 0 -9px;border-radius:50%;' +
				'background:rgb(236 237 238 / .9);box-shadow:0 0 0 2px rgb(11 12 13 / .8);' +
				'pointer-events:none;z-index:2147483647;transition:transform 60ms linear;display:none';
			document.documentElement.append(dot);
			window.addEventListener(
				'mousemove',
				(e) => {
					dot.style.display = 'block';
					dot.style.transform = `translate(${e.clientX}px,${e.clientY}px)`;
				},
				true
			);
			window.addEventListener(
				'mousedown',
				(e) => {
					const ring = document.createElement('div');
					ring.style.cssText =
						`position:fixed;left:${e.clientX}px;top:${e.clientY}px;width:18px;height:18px;margin:-9px 0 0 -9px;` +
						'border-radius:50%;border:2px solid #f5b544;pointer-events:none;z-index:2147483647;';
					document.documentElement.append(ring);
					ring
						.animate(
							[
								{ transform: 'scale(1)', opacity: 0.9 },
								{ transform: 'scale(2.6)', opacity: 0 }
							],
							{ duration: 500, easing: 'ease-out' }
						)
						.finished.then(() => ring.remove());
				},
				true
			);
		};
		if (document.documentElement) draw();
		else window.addEventListener('DOMContentLoaded', draw);
	});
}

/** Moves the cursor to the middle of something, the way a hand would, and then clicks it. */
async function press(page: Page, target: Locator) {
	await glide(page, target);
	await page.waitForTimeout(250);
	await target.click();
}

async function glide(
	page: Page,
	target: Locator,
	at: { x: number; y: number } = { x: 0.5, y: 0.5 }
) {
	const box = await target.boundingBox();
	if (!box) throw new Error('nothing to move to');
	await page.mouse.move(box.x + box.width * at.x, box.y + box.height * at.y, { steps: 28 });
}

test('the demo, filmed', async ({ page }) => {
	test.setTimeout(90_000);
	await addCursor(page);
	await open(page, '/');
	await page.mouse.move(640, 400);
	// The overview: the map, the health score and what needs you.
	await page.waitForTimeout(1500);

	// A machine's history.
	await press(page, page.getByRole('navigation').getByRole('link', { name: 'Devices' }).first());
	await page.getByRole('note').filter({ hasText: 'made-up machines' }).waitFor();
	await page.waitForTimeout(800);
	await press(page, link(page, '/devices/', 'NAS'));
	await page.waitForURL(/\/devices\/[^/]+$/);
	await page.waitForTimeout(900);
	const chart = page.getByRole('slider', { name: /CPU and memory over the last/ }).first();
	await chart.scrollIntoViewIfNeeded();
	await glide(page, chart, { x: 0.3, y: 0.5 });
	await page.waitForTimeout(600);
	await glide(page, chart, { x: 0.72, y: 0.5 });
	await page.waitForTimeout(1800);

	// Containers.
	await press(page, page.getByRole('navigation').getByRole('link', { name: 'Docker' }).first());
	await page.getByRole('note').filter({ hasText: 'made-up machines' }).waitFor();
	await page.waitForTimeout(900);
	await press(page, link(page, '/docker/', 'caddy'));
	await page.waitForTimeout(2400);

	// What needs you.
	await press(page, page.getByRole('navigation').getByRole('link', { name: 'Alerts' }).first());
	await page.waitForTimeout(2000);

	// Search.
	await page.keyboard.press('Control+k');
	await page.waitForTimeout(600);
	await page.keyboard.type('nas', { delay: 120 });
	await page.waitForTimeout(1400);
	await page.keyboard.press('Escape');
	await page.waitForTimeout(400);

	await page.close();
	await page.video()?.saveAs(video);
});
