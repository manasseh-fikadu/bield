// How Bield shows the state of a device, service or check.

import type { Status } from './api/types.js';

/** The state of something Bield watches. The hub sends it. */
export type { Status };

/** A color for badges and pills. Green is the healthy tone, which is lit like a lamp, not green. */
export type Tone = 'green' | 'amber' | 'red' | 'blue' | 'purple' | 'gray';

interface StatusMeta {
	/** The state in a word, for sentences and labels. */
	label: string;
	tone: Tone;
	/** The background class of the state's lamp, when it's lit. */
	dot: string;
	/** The border class of the state's lamp, when it's a ring: offline or unknown. */
	ring: string;
	/** The text class of the state's color. */
	text: string;
}

export const statusMeta: Record<Status, StatusMeta> = {
	online: {
		label: 'Online',
		tone: 'green',
		dot: 'bg-lamp',
		ring: 'border-lamp',
		text: 'text-fg-muted'
	},
	attention: {
		label: 'Attention',
		tone: 'amber',
		dot: 'bg-amber',
		ring: 'border-amber',
		text: 'text-amber-fg'
	},
	offline: {
		label: 'Offline',
		tone: 'red',
		dot: 'bg-red-fg',
		ring: 'border-red-fg',
		text: 'text-red-fg'
	},
	unknown: {
		label: 'Unknown',
		tone: 'gray',
		dot: 'bg-fg-faint',
		ring: 'border-fg-faint',
		text: 'text-fg-subtle'
	}
};

export const toneClass: Record<Tone, string> = {
	green: 'border-lamp/15 bg-lamp/5 text-lamp',
	amber: 'border-amber/30 bg-amber/10 text-amber-fg',
	red: 'border-red/35 bg-red/10 text-red-fg',
	blue: 'border-blue-fg/30 bg-blue/10 text-blue-fg',
	purple: 'border-purple-fg/30 bg-purple-fg/10 text-purple-fg',
	gray: 'border-line-strong bg-hover text-fg-muted'
};
