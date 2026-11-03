// Where things fall on the scroll. The end of the line stays at one height in the window while the
// page scrolls past it, so what it has reached depends only on the scroll. The page ends before the
// line would, so over the last stretch the end of the line moves down the window to meet it. A
// section that fits the window stays still while its scene plays. There's no DOM in here, so it can
// all be tested.

/** A section held still by `position: sticky`. */
export interface Pin {
	/** The `top` to stick at, which is above the window if the section has space over its content. */
	top: number;
	/** How much of the scroll it stays for. */
	dwell: number;
}

export interface Section {
	/** Where it starts on the page. */
	top: number;
	/** How tall its content is, including the empty space above it. */
	height: number;
	/** That empty space. */
	pad: number;
	pin: Pin | null;
}

export interface View {
	/** The height of the window. */
	height: number;
	/** How far the page can scroll. */
	max: number;
	/** How far down the window the end of the line stays, as a fraction. */
	reach: number;
	/** Where the line ends on the page. */
	end: number;
}

/** How far the page has scrolled when a scene starts, and when it ends. */
export interface Scene {
	start: number;
	end: number;
}

/** The space a held section needs above and below it, in pixels. */
const minMargin = 24;

/** A section stays for at least this much of the window, however short it is. */
const minDwell = 0.6;

const lerp = (a: number, b: number, t: number) => a + (b - a) * t;

/** Centers a section's content in the window while it's held, or says no if it doesn't fit. */
export function pinFor(height: number, pad: number, windowHeight: number): Pin | null {
	const shown = height - pad;
	const margin = (windowHeight - shown) / 2;
	if (margin < minMargin) {
		return null;
	}
	return { top: margin - pad, dwell: Math.max(shown, windowHeight * minDwell) };
}

/** Where the scroll starts to take the end of the line down the window. */
function tailOf(view: View): number {
	return Math.max(0, view.max - view.height);
}

/** How far down the window the end of the line is, when the page has scrolled to `scroll`. */
export function tipInWindow(scroll: number, view: View): number {
	const resting = view.reach * view.height;
	const tail = tailOf(view);
	if (scroll <= tail || view.max <= tail) {
		return resting;
	}
	const t = Math.min(1, (scroll - tail) / (view.max - tail));
	// It eases in, so that nothing shows where it starts to move.
	return lerp(resting, Math.max(resting, view.end - view.max), t * t * (3 - 2 * t));
}

/** The scroll at which the end of the line gets to `y` on the page. */
export function scrollFor(y: number, view: View): number {
	const tail = tailOf(view);
	if (y <= tail + tipInWindow(tail, view)) {
		return y - tipInWindow(tail, view);
	}
	if (y >= view.max + tipInWindow(view.max, view)) {
		return view.max;
	}
	let low = tail;
	let high = view.max;
	for (let i = 0; i < 40; i++) {
		const middle = (low + high) / 2;
		if (middle + tipInWindow(middle, view) < y) {
			low = middle;
		} else {
			high = middle;
		}
	}
	return high;
}

/**
 * A held section's scene plays for as long as it's held. Any other plays as the line goes from the
 * top of what shows to the bottom of it.
 */
export function scenesFor(sections: Section[], view: View): Scene[] {
	return sections.map(({ top, height, pad, pin }) => {
		if (pin) {
			const start = top - pin.top;
			return { start, end: start + pin.dwell };
		}
		return { start: scrollFor(top + pad, view), end: scrollFor(top + height, view) };
	});
}
