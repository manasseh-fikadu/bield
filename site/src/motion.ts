// What the page does as you scroll. The line down the page is the only light on it: what's to its
// right stays dark until the line gets there, then wakes up and plays what it's about. All of it is
// driven by the scroll and not the clock, so it runs backwards when you scroll back up. The page
// reads the same without any of this, since nothing is hidden until the script hides it.
//
// The line itself is plain CSS: see index.html. This only moves it for the last stretch of the page.

import { pinFor, scenesFor, scrollFor, tipInWindow, type Scene, type View } from './stage.js';

type Span = [number, number];

/** A paused animation, and where on the scroll it plays. */
interface Step {
	anim: Animation;
	place: () => Span;
	start: number;
	end: number;
	done: number;
	/** How far through it the element is where it belongs, for measuring it. */
	rest: number;
}

/** A section of the page, and its scene. */
interface Act {
	section: HTMLElement;
	content: HTMLElement;
	stop: HTMLElement | null;
	/** How far below the top of the section its stop is, and the scroll at which the line gets there. */
	stopOffset: number;
	stopAt: number;
	held: boolean;
	scene: Scene;
	/** Where the section starts on the page. */
	pageTop: number;
	/** Where what shows of its content starts in the window, and how tall that is. */
	top: number;
	shown: number;
}

/** The line shows on a window at least this wide, and sections only hold still there. */
const railFrom = 1024;

/** How far down the window the line reaches, with and without the rail on show. The first has to match `--tip` in index.html. */
const reachWide = 0.62;
const reachNarrow = 0.85;

/** How quickly the scenes catch up with the scroll, in milliseconds. The line doesn't wait. */
const lag = 90;

/** Every animation runs for this long, and the scroll sets how far through it is. */
const length = 1000;

/** The least scroll an animation gets when it's placed by where it is on the page, and the most. */
const minSpan = 90;
const maxSpan = 260;

const settle = 'cubic-bezier(0.16, 1, 0.3, 1)';

/**
 * How much shows of what the line hasn't reached yet. A mockup stays exactly where it'll be, only
 * dim, so that it's a screen that's off and not an empty frame, and nothing shifts as it comes on.
 */
const unlit = 0.16;

const light: Keyframe[] = [{ opacity: unlit }, { opacity: 1 }];
const lift: Keyframe[] = [
	{ opacity: unlit, transform: 'translateY(8px)' },
	{ opacity: 1, transform: 'none' }
];
const word: Keyframe[] = [
	{ opacity: unlit, transform: 'translateY(0.1em)' },
	{ opacity: 1, transform: 'none' }
];

/** Light going across, with a soft edge. It's a mask, so that what it hasn't reached still shows. */
const sweepMask = {
	maskImage: `linear-gradient(to right, #000 45.45%, rgb(0 0 0 / ${unlit}) 54.55%)`,
	maskSize: '220% 100%',
	maskRepeat: 'no-repeat'
};
const sweep: Keyframe[] = [
	{ ...sweepMask, maskPosition: '100% 0' },
	{ ...sweepMask, maskPosition: '0% 0' }
];

/** Light going down, which with `steps()` is a line at a time. */
const printMask = {
	maskImage: `linear-gradient(to bottom, #000 50%, rgb(0 0 0 / ${unlit}) 50%)`,
	maskSize: '100% 200%',
	maskRepeat: 'no-repeat'
};
const print: Keyframe[] = [
	{ ...printMask, maskPosition: '0 100%' },
	{ ...printMask, maskPosition: '0 0' }
];

const pop: Keyframe[] = [
	{ opacity: unlit, transform: 'none' },
	{ opacity: 1, transform: 'scale(1.5)', offset: 0.6 },
	{ opacity: 1, transform: 'none' }
];

const steps: Step[] = [];
const acts: Act[] = [];
let view: View = { height: 0, max: 0, reach: reachWide, end: 0 };
/** The scroll at which the line starts to fill. */
let lineFrom = 0;

const clamp = (n: number, min: number, max: number) => Math.min(max, Math.max(min, n));

function scrub(
	el: Element,
	frames: Keyframe[],
	place: () => Span,
	easing = 'linear',
	rest = length
) {
	const anim = el.animate(frames, { duration: length, fill: 'both', easing });
	anim.pause();
	steps.push({ anim, place, start: 0, end: 1, done: -1, rest });
}

/** A held section dims and draws back as the page moves on, which leaves the stage to the next. */
function recede(act: Act) {
	scrub(
		act.content,
		[
			{ opacity: 1, transform: 'none' },
			{ opacity: 0.1, transform: 'translateY(-24px) scale(0.975)' }
		],
		// Past the end of the scroll, so it never plays if the section isn't held after all.
		() =>
			act.held ? [act.scene.end, act.scene.end + innerHeight * 0.5] : [view.max + 1, view.max + 2],
		'ease-in',
		0
	);
}

/**
 * Plays between two points of a scene. A section that doesn't hold still plays as the line passes
 * `el`, however its layout came out, since a scene written for one layout can't fit them all.
 */
function during(act: Act, el: Element, when: Span, late = 0): () => Span {
	return () => {
		const { start, end } = act.scene;
		const span = end - start;
		if (act.held) {
			return [start + when[0] * span, start + when[1] * span];
		}
		const box = el.getBoundingClientRect();
		const scale = span / act.shown;
		const a = start + clamp(box.top - act.top, 0, act.shown) * scale + late;
		const b = start + clamp(box.bottom - act.top, 0, act.shown) * scale + late;
		const from = Math.min(a, end - minSpan);
		return [from, Math.min(end, clamp(b, from + minSpan, from + maxSpan))];
	};
}

/** Plays while `el` comes up the window, up to where its section stops or the line would reach it. */
function arriving(act: Act, el: HTMLElement): () => Span {
	return () => {
		const top =
			act.pageTop + el.getBoundingClientRect().top - act.content.getBoundingClientRect().top;
		const from = top - innerHeight;
		return [from, act.held ? act.scene.start : top - innerHeight * 0.55];
	};
}

/** Plays one element after another, each overlapping the next ones. */
function stagger(act: Act, els: Element[], frames: Keyframe[], [from, to]: Span, overlap = 2.5) {
	const gap = (to - from) / Math.max(1, els.length - 1 + overlap);
	els.forEach((el, i) => {
		scrub(el, frames, during(act, el, [from + gap * i, from + gap * (i + overlap)]), settle);
	});
}

function parts(root: Element, name: string): HTMLElement[] {
	return [...root.querySelectorAll<HTMLElement>(`[data-part="${name}"]`)];
}

/** Splits a heading into words that can light one at a time. */
function words(heading: HTMLElement): HTMLElement[] {
	const text = heading.textContent!.trim().split(/\s+/);
	const spans = text.map((w) => {
		const span = document.createElement('span');
		span.className = 'inline-block';
		span.textContent = w;
		return span;
	});
	heading.replaceChildren(...spans.flatMap((s, i) => (i ? [' ', s] : [s])));
	return spans;
}

/**
 * Starts as the line reaches `el`, and later the further `el` is to its right, as if the light
 * spreads out from the line.
 */
function reached(act: Act, el: Element, span = 140): () => Span {
	return () => {
		const box = el.getBoundingClientRect();
		const content = act.content.getBoundingClientRect();
		const start = scrollFor(act.pageTop + box.top - content.top, view);
		const late = (box.left - content.left) * 0.2;
		return [start + late, start + late + span];
	};
}

/** The heading and the paragraphs under it light as the line reaches them, word by word. */
function copy(act: Act) {
	for (const el of words(act.content.querySelector<HTMLElement>('h2')!)) {
		scrub(el, word, reached(act, el), settle);
	}
	for (const el of act.content.querySelectorAll('p')) {
		scrub(el, lift, reached(act, el), settle);
	}
}

/** Adds to a stop what its `data-lit` shows: a ring going out, and a branch across to the heading. */
function dressStop(act: Act) {
	const ring = document.createElement('span');
	ring.className =
		'pointer-events-none absolute -inset-0.5 rounded-full border border-lamp opacity-0 in-data-lit:animate-ripple';
	const branch = document.createElement('span');
	branch.className =
		'pointer-events-none absolute top-[calc(50%-1px)] left-full ml-0.5 h-0.5 w-8 origin-left scale-x-0 bg-lamp transition-[scale] duration-300 in-data-lit:scale-x-100';
	act.stop?.append(ring, branch);
}

/** Brings a mockup up the window, tipped back and dim, until it's flat and ready to play. */
function arrive(act: Act): HTMLElement {
	const figure = act.content.querySelector<HTMLElement>('figure')!;
	figure.classList.add('origin-top');
	scrub(
		figure,
		[
			{
				opacity: 0.2,
				transform: 'perspective(1600px) rotateX(12deg) translateY(64px) scale(0.96)'
			},
			{ opacity: 1, transform: 'none' }
		],
		arriving(act, figure),
		'ease-out'
	);
	return figure;
}

/** A lamp coming on, which glows for a moment if it's bad news. */
function lampOn(act: Act, lamp: Element, when: Span) {
	scrub(lamp, pop, during(act, lamp, when), 'ease-out');
	if (lamp.getAttribute('data-part') === 'alarm') {
		const glow = getComputedStyle(lamp).backgroundColor;
		const color = glow === 'rgba(0, 0, 0, 0)' ? getComputedStyle(lamp).borderTopColor : glow;
		scrub(
			lamp,
			[
				{ boxShadow: '0 0 0 0 transparent' },
				{ boxShadow: `0 0 14px 4px ${color}`, offset: 0.5 },
				{ boxShadow: `0 0 8px 1px ${color}` }
			],
			during(act, lamp, when),
			'ease-out'
		);
	}
}

/** A button drawing the eye, once the scene has got to what it's for. */
function beckon(act: Act, button: Element, when: Span) {
	scrub(
		button,
		[
			{ boxShadow: '0 0 0 0 rgb(236 237 238 / 0.5)' },
			{ boxShadow: '0 0 0 7px rgb(236 237 238 / 0)' }
		],
		during(act, button, when)
	);
}

/** The chart draws itself, up to the spike, and that opens what was running at the time. */
function history(act: Act) {
	copy(act);
	dressStop(act);
	const figure = arrive(act);
	stagger(act, parts(figure, 'head'), light, [0.06, 0.2]);
	stagger(act, parts(figure, 'axis'), light, [0.1, 0.22]);
	const chart = parts(figure, 'chart')[0];
	const from = 0.16;
	const to = 0.68;
	scrub(chart, sweep, during(act, chart, [from, to]));
	// The spike is 78% of the way along, which the edge of the sweep gets to 73% of the way through.
	const spike = from + (to - from) * 0.73;
	scrub(parts(figure, 'spike')[0], pop, during(act, chart, [spike - 0.01, spike + 0.04], 40));
	scrub(
		parts(figure, 'tip')[0],
		[
			{ opacity: 0, transform: 'translateY(12px) scale(0.95)' },
			{ opacity: 1, transform: 'none' }
		],
		during(act, chart, [spike, spike + 0.08], 50),
		settle
	);
	stagger(act, parts(figure, 'tip-row'), light, [spike + 0.03, spike + 0.15]);
	stagger(act, parts(figure, 'minor'), light, [0.68, 0.78]);
	for (const minor of parts(figure, 'minor-chart')) {
		scrub(minor, sweep, during(act, minor, [0.7, 1]));
	}
}

/** The list comes up a row at a time, its bad news lights, and the panel opens on the first of it. */
function docker(act: Act) {
	copy(act);
	dressStop(act);
	const figure = arrive(act);
	stagger(act, parts(figure, 'head'), light, [0.06, 0.16]);

	const trouble = parts(figure, 'trouble');
	let alarms = 0;
	const rows = parts(figure, 'row');
	rows.forEach((row, i) => {
		const t = 0.14 + (0.46 * i) / rows.length;
		scrub(row, light, during(act, row, [t, t + 0.08]), settle);
		const lamp = row.querySelector('[data-part="lamp"], [data-part="alarm"]');
		if (lamp) {
			lampOn(act, lamp, [t + 0.01, t + 0.06]);
		}
		if (lamp?.getAttribute('data-part') === 'alarm') {
			scrub(trouble[alarms++], light, during(act, row, [t, t + 0.05]), settle);
		}
		const spark = row.querySelector('[data-part="spark"]');
		if (spark) {
			scrub(spark, sweep, during(act, row, [t + 0.01, t + 0.1]));
		}
	});

	const selected = figure.querySelector('[data-selected]')!;
	scrub(
		selected,
		[{ backgroundColor: 'transparent' }, { backgroundColor: 'var(--color-raised)' }],
		during(act, selected, [0.6, 0.65])
	);
	const panel = parts(figure, 'panel')[0];
	scrub(panel, sweep, during(act, panel, [0.6, 0.74]));
	stagger(act, parts(figure, 'log'), light, [0.7, 0.96], 2);
	beckon(act, parts(figure, 'action')[0], [0.94, 1]);
}

/** The folders and files come up, the file opens line by line, and the terminal lands over it. */
function shell(act: Act) {
	copy(act);
	dressStop(act);
	const figure = arrive(act);
	stagger(act, parts(figure, 'place'), light, [0.06, 0.32]);
	stagger(act, parts(figure, 'file'), light, [0.14, 0.38]);
	const selected = figure.querySelector('[data-selected]')!;
	scrub(
		selected,
		[{ backgroundColor: 'transparent' }, { backgroundColor: 'var(--color-raised)' }],
		during(act, selected, [0.38, 0.42])
	);
	const preview = parts(figure, 'preview')[0];
	scrub(preview, sweep, during(act, preview, [0.38, 0.5]));
	for (const code of parts(figure, 'code')) {
		scrub(code, print, during(act, code, [0.46, 0.7]), 'steps(18)');
	}
	const term = parts(figure, 'term')[0];
	scrub(
		term,
		[
			{ opacity: 0, transform: 'translateY(40px) scale(0.96)' },
			{ opacity: 1, transform: 'none' }
		],
		during(act, term, [0.66, 0.78]),
		settle
	);
	scrub(parts(figure, 'term-text')[0], print, during(act, term, [0.74, 1], 60), 'steps(5)');
}

/** Each alert lands with its lamp coming on, while the ways it can reach you light in turn. */
function alerts(act: Act) {
	copy(act);
	dressStop(act);
	const figure = arrive(act);
	stagger(act, parts(figure, 'head'), light, [0.08, 0.16]);
	const cards = parts(figure, 'card');
	cards.forEach((card, i) => {
		const t = 0.16 + (0.62 * i) / cards.length;
		scrub(card, sweep, during(act, card, [t, t + 0.12]));
		lampOn(act, card.querySelector('[data-part="lamp"], [data-part="alarm"]')!, [
			t + 0.03,
			t + 0.1
		]);
	});
	beckon(act, parts(figure, 'action')[0], [0.86, 0.96]);
	stagger(act, [...act.content.querySelectorAll('li')], light, [0.26, 0.96]);
}

/** The check that logs you in prints a line at a time, as the line goes by. */
function login(act: Act) {
	copy(act);
	dressStop(act);
	stagger(act, [...act.content.querySelectorAll('dl')], light, [0, 1], 1);
	stagger(act, [...act.content.querySelectorAll('dl > div')], sweep, [0, 1]);
	stagger(act, [...act.content.querySelectorAll('li')], sweep, [0, 1]);
}

/** The window comes up, and the commands print in it. */
function install(act: Act) {
	copy(act);
	dressStop(act);
	const box = act.content.querySelector<HTMLElement>('[data-install]')!;
	scrub(box, lift, during(act, box, [0, 1]), settle);
	// The commands on the other tabs are hidden, so they go by where the window is.
	for (const code of act.content.querySelectorAll('code')) {
		const lines = code.textContent!.split('\n').length;
		scrub(code, print, during(act, box, [0, 1], 50), `steps(${lines})`);
	}
	stagger(act, [...act.content.querySelectorAll('li')], sweep, [0, 1]);
}

const sceneOf: Record<string, (act: Act) => void> = {
	history,
	docker,
	shell,
	alerts,
	login,
	install
};

/** The headline drifts back, and the dashboard under it comes flat, as the page starts to move. */
function hero() {
	const figure = document.querySelector<HTMLElement>('[data-demo]');
	if (figure) {
		scrub(
			figure,
			[{ transform: 'perspective(1800px) rotateX(13deg) scale(0.965)' }, { transform: 'none' }],
			// Flat by the time it's filling the window, so that its text is sharp.
			() => [0, Math.max(innerHeight * 0.3, (figure.getBoundingClientRect().top + scrollY) * 0.85)],
			'ease-out'
		);
	}
	const headline = document.querySelector('h1');
	if (headline) {
		scrub(
			headline,
			[
				{ transform: 'none', opacity: 1 },
				{ transform: 'translateY(56px)', opacity: 0.25 }
			],
			() => [0, innerHeight * 0.8]
		);
	}
}

/** The amber lamp at the end of the line blooms as the page ends. */
function ending() {
	const end = document.querySelector('[data-end]');
	if (end) {
		scrub(
			end,
			[
				{
					opacity: 0.35,
					transform: 'scale(0.55)',
					boxShadow: '0 0 0 0 rgb(245 181 68 / 0)'
				},
				{
					opacity: 1,
					transform: 'none',
					boxShadow: '0 0 44px 12px rgb(245 181 68 / 0.45)'
				}
			],
			() => [view.max - 110, view.max],
			settle
		);
	}
	const closing = document.querySelector('footer > :last-child');
	if (closing) {
		scrub(closing, lift, () => [view.max - 240, view.max - 40], settle);
	}
}

/** Decides what's held still, and works out where everything plays on the scroll from that. */
function layout() {
	// Put everything where it belongs first, since that's what has to be measured.
	for (const step of steps) {
		step.anim.currentTime = step.rest;
	}
	const wide = innerWidth >= railFrom;
	const sections = acts.map((act) => {
		const { section, content, stop } = act;
		const height = content.offsetHeight;
		const pad = parseFloat(getComputedStyle(content).paddingTop);
		const pin = wide && section.hasAttribute('data-pin') ? pinFor(height, pad, innerHeight) : null;
		act.held = pin !== null;
		section.style.minHeight = pin ? `${height + pin.dwell}px` : '';
		content.style.position = pin ? 'sticky' : '';
		content.style.top = pin ? `${pin.top}px` : '';
		if (stop) {
			const margin = parseFloat(getComputedStyle(stop).marginTop);
			act.stopOffset = margin + stop.offsetHeight / 2;
			stop.style.position = pin ? 'sticky' : '';
			stop.style.top = pin ? `${pin.top + margin}px` : '';
			// Sticky stays inside its parent, so this has the stop leave just as the heading does.
			const column = stop.parentElement!;
			column.style.alignSelf = pin ? 'flex-start' : '';
			column.style.height = pin ? `${pin.dwell + margin + stop.offsetHeight}px` : '';
		}
		return { height, pad, pin, top: 0 };
	});
	acts.forEach((act, i) => {
		const box = act.content.getBoundingClientRect();
		act.pageTop = act.section.getBoundingClientRect().top + scrollY;
		act.top = box.top + sections[i].pad;
		act.shown = sections[i].height - sections[i].pad;
		sections[i].top = act.pageTop;
	});

	const page = document.documentElement.scrollHeight;
	const line = document.querySelector<HTMLElement>('[data-line]');
	const rail = line?.offsetHeight ? line.getBoundingClientRect() : null;
	view = {
		height: innerHeight,
		max: page - innerHeight,
		reach: wide ? reachWide : reachNarrow,
		end: rail ? rail.bottom + scrollY : page
	};
	lineFrom = scrollFor(rail ? rail.top + scrollY : (sections[0]?.top ?? 0), view);
	const scenes = scenesFor(sections, view);
	acts.forEach((act, i) => {
		act.scene = scenes[i];
		act.stopAt = scrollFor(act.pageTop + act.stopOffset, view);
	});

	for (const step of steps) {
		const [start, end] = step.place();
		step.start = start;
		step.end = Math.max(start + 1, end);
		step.done = -1;
	}
}

/** Plays the scenes, a little behind the scroll, which gives them weight. */
function draw(position: number) {
	// The page's height gets rounded, so the scroll can stop a fraction short of its end.
	const scroll = view.max - position < 1 ? view.max : position;
	for (const step of steps) {
		const done = clamp((scroll - step.start) / (step.end - step.start), 0, 1);
		if (done !== step.done) {
			step.done = done;
			step.anim.currentTime = done * length;
		}
	}
}

/**
 * What the line needs from a script, with no delay, so that a stop lights just as the line touches
 * it. Until the last stretch of the page, `--tip` doesn't change, so nothing here moves the line.
 */
function drawLine() {
	const scroll = view.max - scrollY < 1 ? view.max : scrollY;
	document
		.querySelector<HTMLElement>('[data-line]')
		?.style.setProperty('--tip', `${tipInWindow(scroll, view)}px`);
	// It's hidden until the line starts, and once it has gone into the lamp at the end.
	document
		.querySelector('[data-line-head]')
		?.classList.toggle('opacity-0', scroll < lineFrom || scroll >= view.max);
	for (const { stop, stopAt } of acts) {
		stop?.toggleAttribute('data-lit', scroll >= stopAt);
	}
}

/** For a visitor who asked for less motion: the stops are simply lit, and nothing moves. */
function still() {
	for (const stop of document.querySelectorAll('[data-stop]')) {
		stop.setAttribute('data-lit', '');
	}
}

export function startMotion() {
	if (matchMedia('(prefers-reduced-motion: reduce)').matches) {
		still();
		return;
	}
	for (const section of document.querySelectorAll<HTMLElement>('main section')) {
		const content = section.lastElementChild as HTMLElement;
		if (section.hasAttribute('data-pin')) {
			// So that it's its own height, and has the rest of the section to stick in.
			content.style.alignSelf = 'flex-start';
		}
		const act: Act = {
			section,
			content,
			stop: section.querySelector('[data-stop]'),
			stopOffset: 0,
			stopAt: 0,
			held: false,
			scene: { start: 0, end: 1 },
			pageTop: 0,
			top: 0,
			shown: 1
		};
		acts.push(act);
		sceneOf[section.getAttribute('aria-labelledby') ?? '']?.(act);
		if (section.hasAttribute('data-pin')) {
			recede(act);
		}
	}
	hero();
	ending();

	let shown = scrollY;
	let frame = 0;
	let then = 0;
	const tick = (now: number) => {
		const k = 1 - Math.exp(-(now - then) / lag);
		then = now;
		shown += (scrollY - shown) * k;
		if (Math.abs(scrollY - shown) < 0.25) {
			shown = scrollY;
		}
		draw(shown);
		frame = shown === scrollY ? 0 : requestAnimationFrame(tick);
	};
	addEventListener(
		'scroll',
		() => {
			drawLine();
			if (!frame) {
				then = performance.now();
				frame = requestAnimationFrame(tick);
			}
		},
		{ passive: true }
	);

	// The page changes height as fonts load and the demo replaces its picture.
	let pending = 0;
	const again = () => {
		pending ||= requestAnimationFrame(() => {
			pending = 0;
			layout();
			shown = scrollY;
			draw(shown);
			drawLine();
		});
	};
	let width = innerWidth;
	addEventListener('resize', () => {
		// On a phone the window's height changes as its address bar comes and goes.
		if (innerWidth !== width || innerWidth >= railFrom) {
			width = innerWidth;
			again();
		}
	});
	new ResizeObserver(again).observe(document.body);
	void document.fonts.ready.then(again);

	layout();
	draw(shown);
	drawLine();
}
