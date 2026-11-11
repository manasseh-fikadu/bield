// How the app shows alerts, the activity log, and the services on the hub:
// its containers and systemd units.

import type {
	Alert,
	AlertSeverity,
	Container,
	Device,
	Event,
	EventKind,
	EventLevel,
	Unit
} from './api/types.js';
import type { Status, Tone } from './status.js';

interface SeverityMeta {
	label: string;
	tone: Tone;
	/** The text class of its color. */
	text: string;
	/** The classes of a card's border and background. */
	box: string;
}

export const severityMeta: Record<AlertSeverity, SeverityMeta> = {
	critical: {
		label: 'Critical',
		tone: 'red',
		text: 'text-red-fg',
		box: 'border-red/30 bg-red/[0.06]'
	},
	warning: {
		label: 'Warning',
		tone: 'amber',
		text: 'text-amber-fg',
		box: 'border-amber/25 bg-amber/[0.05]'
	},
	info: {
		label: 'Info',
		tone: 'blue',
		text: 'text-blue-fg',
		box: 'border-blue-fg/20 bg-blue/[0.05]'
	}
};

/** How long an admin can snooze an alert for, in a menu. */
export const snoozeOptions: { minutes: number; label: string }[] = [
	{ minutes: 60, label: '1 hour' },
	{ minutes: 240, label: '4 hours' },
	{ minutes: 1440, label: '1 day' },
	{ minutes: 10080, label: '1 week' }
];

/** The alerts that need attention now: open or acknowledged, and not snoozed. */
export function activeAlerts(alerts: Alert[]): Alert[] {
	return alerts.filter((a) => a.state === 'open' || a.state === 'acknowledged');
}

/** The alerts nobody has acknowledged or snoozed yet: the things that need someone. */
export function openAlerts(alerts: Alert[]): Alert[] {
	return alerts.filter((a) => a.state === 'open');
}

/** The IDs of the devices with an alert that hasn't resolved. */
export function alertedDevices(alerts: Alert[]): Set<string> {
	return new Set(alerts.flatMap((a) => (a.deviceId && a.state !== 'resolved' ? [a.deviceId] : [])));
}

/**
 * Whether a device is off and nobody needs to know, such as a tablet in a
 * drawer. Its lamp is a grey ring instead of a red one.
 */
export function quietlyOffline(d: Device, alerted: Set<string>): boolean {
	return !d.online && !alerted.has(d.id);
}

/**
 * Devices with what needs someone first: offline with an alert, then needing
 * attention, then the healthy ones, and last the ones that are quietly off.
 * Devices that tie stay in the order they came.
 */
export function troubleFirst(devices: Device[], alerted: Set<string>): Device[] {
	const rank = (d: Device) => {
		if (quietlyOffline(d, alerted)) {
			return 4;
		}
		return { offline: 0, attention: 1, online: 2, unknown: 3 }[d.status];
	};
	return devices.toSorted((a, b) => rank(a) - rank(b));
}

/** The tabs of the Alerts page. */
export type AlertTab = 'open' | 'snoozed' | 'resolved';

/** The tab in a URL's ?tab=, or the open alerts. */
export function parseAlertTab(s: string | null): AlertTab {
	return s === 'snoozed' || s === 'resolved' ? s : 'open';
}

/**
 * Whether a device is the machine Bield runs on. In embedded mode, that
 * machine is two nodes: Bield's own, and the machine's.
 */
export function onHub(d: Device): boolean {
	return d.measuredBy === 'hub';
}

/**
 * The IDs a device's alerts, units and containers can be under: its own,
 * and on the hub's machine, its other node's.
 */
export function deviceIds(device: Device, devices: Device[]): Set<string> {
	if (!onHub(device)) {
		return new Set([device.id]);
	}
	return new Set(devices.filter(onHub).map((d) => d.id));
}

/** The items about a device, such as its alerts or units. */
export function onDevice<T extends { deviceId?: string }>(
	items: T[],
	device: Device,
	devices: Device[]
): T[] {
	const ids = deviceIds(device, devices);
	return items.filter((i) => i.deviceId !== undefined && ids.has(i.deviceId));
}

/** A systemd unit's state as a status. */
export function unitStatus(u: Unit): Status {
	switch (u.active) {
		case 'active':
			return 'online';
		case 'failed':
			return 'offline';
		case 'activating':
		case 'reloading':
		case 'deactivating':
			return 'attention';
	}
	return u.watched ? 'attention' : 'unknown';
}

/** A container's state as a status. */
export function containerStatus(c: Container): Status {
	if (c.problem) {
		return c.state === 'running' || c.state === 'restarting' || c.problem === 'stopped'
			? 'attention'
			: 'offline';
	}
	return c.state === 'running' ? 'online' : 'unknown';
}

/** The containers of one compose project, or of none. */
export interface ContainerGroup {
	/** The project, or null for the containers in none. */
	project: string | null;
	containers: Container[];
}

/** Groups containers by compose project, by name, and then those in none. */
export function groupContainers(containers: Container[]): ContainerGroup[] {
	const projects = new Map<string, Container[]>();
	const loose: Container[] = [];
	for (const c of containers) {
		if (!c.project) {
			loose.push(c);
			continue;
		}
		const list = projects.get(c.project);
		if (list) {
			list.push(c);
		} else {
			projects.set(c.project, [c]);
		}
	}
	const out: ContainerGroup[] = [...projects]
		.sort(([a], [b]) => a.localeCompare(b))
		.map(([project, containers]) => ({ project, containers }));
	if (loose.length > 0) {
		out.push({ project: null, containers: loose });
	}
	return out;
}

export const eventKindLabels: Record<EventKind, string> = {
	device: 'Devices',
	agent: 'Agents',
	alert: 'Alerts',
	check: 'Checks',
	container: 'Containers',
	access: 'Access',
	settings: 'Settings',
	ssh: 'SSH',
	terminal: 'Terminals',
	files: 'Files',
	notification: 'Notifications',
	editor: 'VS Code'
};

/**
 * The kinds of event only admins see: who has a role, SSH access, terminals,
 * files and VS Code.
 */
export const adminEventKinds: EventKind[] = ['access', 'ssh', 'terminal', 'files', 'editor'];

/**
 * Who did an event and how, such as "by alice@example.com with the token
 * deploy, with the CLI", or '' if no one did.
 */
export function eventActor(e: Pick<Event, 'actor' | 'token' | 'via'>): string {
	if (!e.actor) {
		return '';
	}
	const how = [
		e.token ? `with the token ${e.token}` : '',
		e.via === 'cli' ? 'with the CLI' : e.via === 'mcp' ? 'with MCP' : ''
	].filter(Boolean);
	return [`by ${e.actor}${how.length > 0 ? ` ${how[0]}` : ''}`, ...how.slice(1)].join(', ');
}

/** The text class of an event's level. */
export const eventLevelText: Record<EventLevel, string> = {
	info: 'text-fg-subtle',
	success: 'text-fg-muted',
	warning: 'text-amber-fg',
	error: 'text-red-fg'
};

/** An event's level as a status, for its dot. */
export const eventLevelStatus: Record<EventLevel, Status> = {
	info: 'unknown',
	success: 'online',
	warning: 'attention',
	error: 'offline'
};

/**
 * Merges a fresh page of items, such as events or resolved alerts, into the
 * ones loaded: once each, the fresh one of an ID winning, newest (highest
 * ID) first.
 */
export function mergeById<T extends { id: number }>(loaded: T[], fresh: T[]): T[] {
	const ids = new Set(fresh.map((x) => x.id));
	return [...fresh, ...loaded.filter((x) => !ids.has(x.id))].sort((a, b) => b.id - a.id);
}
