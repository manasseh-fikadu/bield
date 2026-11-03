// What the page needs a script for: the mockups scale to fit, the real dashboard takes over from
// the first of them, the install commands switch and copy, and everything in motion.ts. The page
// reads the same without any of it.

import { startMotion } from './motion.js';
import './style.css';

/** A mockup won't shrink past this. A phone scrolls it sideways instead of showing it unreadable. */
const minScale = 0.5;

/** In a frame narrower than this, the dashboard lays itself out, instead of being shrunk to fit. */
const ownLayoutBelow = 900;
const ownLayoutHeight = 640;

/** How long the dashboard gets to draw before the page gives up on it, in milliseconds. */
const appWait = 20_000;

function fitMockups() {
	for (const mock of document.querySelectorAll<HTMLElement>('[data-mock]')) {
		const inner = mock.firstElementChild as HTMLElement;
		const width = Number(mock.dataset.width);
		const height = Number(mock.dataset.height);
		inner.style.width = `${width}px`;
		inner.style.height = `${height}px`;
		const fit = () => {
			const scale = Math.max(minScale, Math.min(1, mock.clientWidth / width));
			// Zoom changes the layout, unlike a transform, so the frame takes the height it needs.
			inner.style.setProperty('zoom', String(scale));
			mock.dataset.scale = String(scale);
		};
		new ResizeObserver(fit).observe(mock);
		fit();
		// A mockup too wide to fit starts at what it's about, past the sidebar.
		mock.scrollLeft = Number(mock.dataset.start ?? 0) * Number(mock.dataset.scale);
	}
}

/**
 * Puts the real dashboard in the hero, once it's there to load: `make site-demo` builds it. The
 * mockup stays until it has drawn, and stays for good if it can't load. Scaling an iframe has to
 * be a transform: with zoom, clicks land in the wrong place.
 */
async function liveDemo() {
	const mock = document.querySelector<HTMLElement>('[data-demo]');
	const inner = mock?.firstElementChild as HTMLElement | undefined;
	if (!mock || !inner) {
		return;
	}
	try {
		const res = await fetch('demo/manifest.json');
		if (!res.ok || !res.headers.get('content-type')?.includes('json')) {
			return;
		}
	} catch {
		return;
	}
	const width = Number(mock.dataset.width);
	const height = Number(mock.dataset.height);
	const frame = document.createElement('iframe');
	let ready = false;

	const fit = () => {
		const own = mock.clientWidth < ownLayoutBelow;
		const scale = own ? 1 : Math.min(1, mock.clientWidth / width);
		const tall = own ? ownLayoutHeight : height;
		frame.style.width = own ? '100%' : `${width}px`;
		frame.style.height = `${tall}px`;
		frame.style.transform = `scale(${scale})`;
		if (ready) {
			mock.style.height = `${tall * scale + (mock.offsetHeight - mock.clientHeight)}px`;
		}
	};

	frame.src = 'demo/';
	frame.title = "Bield's dashboard, on a made-up tailnet";
	frame.className =
		'pointer-events-none absolute top-0 left-0 origin-top-left border-0 opacity-0 transition-opacity duration-300';
	const show = () => {
		ready = true;
		inner.hidden = true;
		mock.style.overflow = 'hidden';
		mock.removeAttribute('role');
		mock.removeAttribute('aria-label');
		frame.classList.remove('pointer-events-none', 'opacity-0');
		for (const link of document.querySelectorAll<HTMLElement>('[data-when]')) {
			link.hidden = link.dataset.when !== 'live';
		}
		fit();
	};
	// The page loading says little: the app is in the files it asks for next. Until it has drawn its
	// first numbers, the mockup stays, and if that never happens, it stays for good.
	const giveUp = Date.now() + appWait;
	const check = setInterval(() => {
		if (frame.contentDocument?.querySelector('main h1')) {
			clearInterval(check);
			show();
		} else if (Date.now() > giveUp) {
			clearInterval(check);
			frame.remove();
		}
	}, 100);
	mock.classList.add('relative');
	mock.append(frame);
	new ResizeObserver(fit).observe(mock);
	fit();
}

/** The install commands: a tab for each way, and a button that copies what's showing. */
function installTabs() {
	const tabs = [...document.querySelectorAll<HTMLButtonElement>('[role="tab"]')];
	const select = (tab: HTMLButtonElement) => {
		for (const t of tabs) {
			const on = t === tab;
			t.setAttribute('aria-selected', String(on));
			t.tabIndex = on ? 0 : -1;
			t.classList.toggle('font-medium', on);
			t.classList.toggle('text-fg', on);
			t.classList.toggle('text-fg-muted', !on);
			document.getElementById(t.getAttribute('aria-controls')!)!.hidden = !on;
		}
	};
	tabs.forEach((tab, i) => {
		tab.addEventListener('click', () => select(tab));
		tab.addEventListener('keydown', (e) => {
			const step = e.key === 'ArrowRight' ? 1 : e.key === 'ArrowLeft' ? -1 : 0;
			if (step) {
				const next = tabs[(i + step + tabs.length) % tabs.length];
				select(next);
				next.focus();
			}
		});
	});

	const copy = document.querySelector<HTMLButtonElement>('[data-copy]');
	copy?.addEventListener('click', async () => {
		const shown = document.querySelector<HTMLElement>('[role="tabpanel"]:not([hidden]) code');
		if (!shown) {
			return;
		}
		try {
			await navigator.clipboard.writeText(shown.innerText);
		} catch {
			return;
		}
		copy.textContent = 'Copied ✓';
		setTimeout(() => (copy.textContent = 'Copy'), 1500);
	});
}

fitMockups();
installTabs();
void liveDemo();
try {
	startMotion();
} catch (err) {
	// Everything starts out dark, so if the scenes can't run, the page has to go back to normal.
	for (const anim of document.getAnimations()) {
		anim.cancel();
	}
	console.error(err);
}
