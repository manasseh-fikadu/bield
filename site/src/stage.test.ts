import { describe, expect, test } from 'bun:test';
import { pinFor, scenesFor, scrollFor, tipInWindow, type Section, type View } from './stage.js';

const view: View = { height: 900, max: 9000, reach: 0.62, end: 9850 };
const resting = 0.62 * 900;

describe('pinFor', () => {
	test.each([
		// height, pad, window, the top to stick at, how long it stays
		[520, 120, 900, 130, 540],
		[800, 120, 900, -10, 680],
		[300, 0, 1400, 550, 840]
	])('holds %p tall content with %p over it in a %p window', (height, pad, win, top, dwell) => {
		expect(pinFor(height, pad, win)).toEqual({ top, dwell });
	});

	test('centers what shows, not the empty space over it', () => {
		const pin = pinFor(520, 120, 900)!;
		const shownTop = pin.top + 120;
		expect(shownTop).toBe(900 - (shownTop + 400));
	});

	test('does not hold content too tall for the window', () => {
		expect(pinFor(1000, 120, 900)).toBeNull();
		expect(pinFor(880, 0, 900)).toBeNull();
	});
});

describe('tipInWindow', () => {
	test('stays at exactly one height until the last stretch of the page', () => {
		for (let s = 0; s <= view.max - view.height; s += 7) {
			expect(tipInWindow(s, view)).toBe(resting);
		}
	});

	test('gets to the end of the line exactly as the page ends', () => {
		expect(view.max + tipInWindow(view.max, view)).toBeCloseTo(view.end);
	});

	test('over the last stretch, only ever moves down the window, and never jumps', () => {
		let last = tipInWindow(view.max - view.height, view);
		for (let s = view.max - view.height + 1; s <= view.max; s++) {
			const tip = tipInWindow(s, view);
			expect(tip).toBeGreaterThanOrEqual(last);
			expect(tip - last).toBeLessThan(1);
			last = tip;
		}
	});

	test('starts moving gently, so that nothing shows where it starts', () => {
		const start = view.max - view.height;
		// A straight line would have moved 3 pixels by here.
		expect(tipInWindow(start + 10, view) - resting).toBeLessThan(0.2);
	});

	test('stays put if the line ends higher than it', () => {
		const short: View = { ...view, end: view.max + 100 };
		expect(tipInWindow(view.max, short)).toBe(resting);
	});
});

describe('scrollFor', () => {
	test('undoes tipInWindow, on the way down and over the last stretch', () => {
		for (let s = 100; s <= view.max; s += 37) {
			const y = s + tipInWindow(s, view);
			expect(scrollFor(y, view)).toBeCloseTo(s, 4);
		}
	});

	test.each([
		// The line can't get past its end, or before it reaches the window.
		[view.end + 500, view.max],
		[view.end, view.max],
		[resting, 0],
		[100, 100 - resting]
	])('takes the end of the line to %p at a scroll of %p', (y, want) => {
		expect(scrollFor(y, view)).toBeCloseTo(want);
	});
});

describe('scenesFor', () => {
	test('runs a held scene for as long as it stays', () => {
		const section: Section = { top: 1200, height: 520, pad: 120, pin: pinFor(520, 120, 900) };
		const [scene] = scenesFor([section], view);
		expect(scene.start).toBe(1200 - 130);
		expect(scene.end).toBe(1200 - 130 + 540);
	});

	test('starts a free scene when the line reaches it, and ends it when the line has passed', () => {
		const section: Section = { top: 2000, height: 600, pad: 100, pin: null };
		const [scene] = scenesFor([section], view);
		expect(scene.start).toBeCloseTo(2100 - resting);
		expect(scene.end).toBeCloseTo(2600 - resting);
	});

	test('finishes every scene before the page ends', () => {
		const short: View = { height: 900, max: 2400, reach: 0.62, end: 3000 };
		const last: Section = { top: 2500, height: 460, pad: 80, pin: null };
		const [scene] = scenesFor([last], short);
		expect(scene.end).toBeLessThan(short.max);
		expect(scene.end + tipInWindow(scene.end, short)).toBeCloseTo(2960, 4);
	});
});
