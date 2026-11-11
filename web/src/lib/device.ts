// How the UI shows a device.

import Cpu from '@lucide/svelte/icons/cpu';
import Laptop from '@lucide/svelte/icons/laptop';
import Monitor from '@lucide/svelte/icons/monitor';
import Server from '@lucide/svelte/icons/server';
import Smartphone from '@lucide/svelte/icons/smartphone';
import Tablet from '@lucide/svelte/icons/tablet';
import type { Device, DeviceKind, Thresholds } from './api/types.js';
import type { Icon } from './icon.js';

const kindIcons: Record<DeviceKind, Icon> = {
	server: Server,
	laptop: Laptop,
	desktop: Monitor,
	tablet: Tablet,
	phone: Smartphone,
	other: Cpu
};

/** An icon for a device, from its kind. */
export function deviceIcon(d: Device): Icon {
	return kindIcons[d.kind];
}

const kindLabels: Record<DeviceKind, string> = {
	server: 'Server',
	laptop: 'Laptop',
	desktop: 'Desktop',
	tablet: 'Tablet',
	phone: 'Phone',
	other: 'Other'
};

/** The kinds of device, in the order the hub lists them. */
export const deviceKinds = Object.keys(kindLabels) as DeviceKind[];

/** A kind of device's name, such as Laptop. */
export function kindLabel(kind: DeviceKind): string {
	return kindLabels[kind];
}

/** The first part of a device's DNS name, such as laptop for laptop.example.ts.net. */
export function shortName(d: Device): string {
	return d.dnsName.split('.')[0];
}

/** When a machine's numbers need attention, in percent. */
export interface Limits {
	cpu: number;
	memory: number;
	disk: number;
	diskCritical: number;
}

/** A device's limits: its own, and where it has none, the hub's. */
export function limitsOf(d: Device, t: Thresholds): Limits {
	const own = d.settings.thresholds;
	return {
		cpu: own.cpuWarn ?? t.cpuWarn,
		memory: own.memoryWarn ?? t.memoryWarn,
		disk: own.diskWarn ?? t.diskWarn,
		diskCritical: own.diskCritical ?? t.diskCritical
	};
}

/** The text class of a percentage: amber from `warn`, red from `crit`, and otherwise `fine`. */
export function levelText(
	value: number | null | undefined,
	warn: number,
	crit = Infinity,
	fine = 'text-fg'
): string {
	if (value == null) {
		return 'text-fg-faint';
	}
	return value >= crit ? 'text-red-fg' : value >= warn ? 'text-amber' : fine;
}

/** A Tailscale tag without its "tag:" start, such as server for tag:server. */
export function tagName(tag: string): string {
	return tag.replace(/^tag:/, '');
}

/** One Tailscale tag and how many devices have it. */
export interface TagCount {
	tag: string;
	count: number;
}

/** Every Tailscale tag the devices have, in alphabetical order, with how many have each. */
export function tagCounts(devices: Device[]): TagCount[] {
	const counts = new Map<string, number>();
	for (const d of devices) {
		for (const tag of new Set(d.tags)) {
			counts.set(tag, (counts.get(tag) ?? 0) + 1);
		}
	}
	return [...counts]
		.map(([tag, count]) => ({ tag, count }))
		.toSorted((a, b) => (a.tag < b.tag ? -1 : a.tag > b.tag ? 1 : 0));
}
