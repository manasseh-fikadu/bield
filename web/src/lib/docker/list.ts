// What the Docker page works out from the lists it loads: which containers to
// show, how each one reads, and the totals on the other tabs. Pure, so the
// components only draw.

import type {
	ContainerPort,
	DockerContainerRow,
	DockerDisk,
	DockerImage,
	DockerNetwork,
	DockerOverview,
	DockerStats,
	DockerVolume
} from '#lib/api/types.js';
import { ago, dateTime, plural } from '#lib/format.js';
import { isUp, matchesFilter, portLabel, rowStatus } from './docker.js';

/** The tabs of the Docker page. */
export const tabs = ['containers', 'volumes', 'images', 'networks'] as const;
export type Tab = (typeof tabs)[number];

/** The tab in ?tab=, or the containers if it names none. */
export function parseTab(value: string | null): Tab {
	return tabs.find((t) => t === value) ?? 'containers';
}

/**
 * The containers to list: the ones that match the filter, and, unless stopped
 * ones are on, only those that are up. A container that's restarting or paused
 * isn't stopped, so it always shows.
 */
export function visibleRows(
	rows: DockerContainerRow[],
	query: string,
	showStopped: boolean
): DockerContainerRow[] {
	return rows.filter((c) => (showStopped || isUp(c.state)) && matchesFilter(c, query));
}

/**
 * How much of the CPUs a container can see it uses, in percent, for its bar.
 * Its CPU figure stays as `docker stats` says it: 100% is one CPU.
 */
export function cpuShare(s: Pick<DockerStats, 'cpu' | 'cpus'> | null | undefined): number | null {
	return s?.cpu == null ? null : s.cpu / Math.max(1, s.cpus);
}

/**
 * How much of its memory limit a container uses, in percent. Without a limit
 * of its own, the limit in its stats is the machine's memory, so this is its
 * share of the machine.
 */
export function memShare(
	s: Pick<DockerStats, 'memUsed' | 'memLimit'> | null | undefined
): number | null {
	return s?.memUsed != null && s.memLimit ? (s.memUsed / s.memLimit) * 100 : null;
}

/** Total CPU across containers, in percent of one CPU, as a share of `cpus` CPUs, or null if unknown. */
export function cpuMeter(cpu: number, cpus: number | null | undefined): number | null {
	return cpus && cpus > 0 ? cpu / cpus : null;
}

/** Whether an RFC 3339 time is one Docker set: it writes year 1 for a container that never ran. */
function known(t: string | undefined): t is string {
	return !!t && Date.parse(t) > 0;
}

/** How long a container has been in its state: "up since Oct 17, 9:36 PM", "stopped 3h ago", or Docker's own words. */
export function uptimeLabel(
	c: Pick<DockerContainerRow, 'state' | 'status' | 'startedAt' | 'finishedAt'>,
	now: number
): string {
	if ((c.state === 'running' || c.state === 'paused') && known(c.startedAt)) {
		return `up since ${dateTime(c.startedAt)}`;
	}
	if ((c.state === 'exited' || c.state === 'dead') && known(c.finishedAt)) {
		return `stopped ${ago(c.finishedAt, now)}`;
	}
	return c.status;
}

/** A port as a chip. */
export interface PortChip {
	/** What it says, such as 8080→80/udp. */
	label: string;
	/** The same with the host address it is published on, if it has one. */
	title: string;
	/** Whether the host publishes it. */
	published: boolean;
}

/**
 * The ports to show in a cell, at most `max`: the published ones, or the
 * container's own if none is published, without repeats, and how many more
 * there are.
 */
export function portChips(ports: ContainerPort[], max = 2): { shown: PortChip[]; more: number } {
	const published = ports.filter((p) => p.public != null);
	const chips: PortChip[] = [];
	for (const p of published.length > 0 ? published : ports) {
		const label = portLabel(p);
		if (!chips.some((c) => c.label === label)) {
			chips.push({
				label,
				title: p.public != null && p.ip ? `${p.ip}:${label}` : label,
				published: p.public != null
			});
		}
	}
	return { shown: chips.slice(0, max), more: Math.max(0, chips.length - max) };
}

/** The disk space that removing unused images and volumes would free. */
export function reclaimable(disk: DockerDisk): number {
	return disk.images.reclaimable + disk.volumes.reclaimable;
}

/**
 * Wraps the request for a daemon's overview for a poll that asks every few
 * seconds. While the daemon hasn't measured its disk, each ask goes to the
 * server, so the disk shows soon after it's known. Once it has, the answer
 * stands for `slow` milliseconds.
 */
export function pacedOverview(
	fetch: () => Promise<DockerOverview>,
	slow = 30_000,
	now: () => number = Date.now
): () => Promise<DockerOverview> {
	let last: { at: number; value: DockerOverview } | null = null;
	return async () => {
		if (last?.value.disk && now() - last.at < slow) {
			return last.value;
		}
		const value = await fetch();
		last = { at: now(), value };
		return value;
	};
}

/** A volume's name for showing: an anonymous volume's 64 hex digits are cut to 12. */
export function volumeName(name: string): string {
	return /^[0-9a-f]{64}$/.test(name) ? `${name.slice(0, 12)}…` : name;
}

/** What the volumes add up to. The size of a volume is missing until the daemon has measured it. */
export function volumeTotals(volumes: DockerVolume[]) {
	return {
		count: volumes.length,
		size: volumes.reduce((n, v) => n + (v.size ?? 0), 0),
		measured: volumes.some((v) => v.size != null),
		unused: volumes.filter((v) => v.usedBy.length === 0).length
	};
}

/** What the images add up to. */
export function imageTotals(images: DockerImage[]) {
	return {
		count: images.length,
		size: images.reduce((n, i) => n + i.size, 0),
		unused: images.filter((i) => i.usedBy.length === 0).length,
		dangling: images.filter((i) => i.dangling).length
	};
}

/** The part of an image digest that tells it apart: `sha256:` and 12 hex digits. */
export function shortDigest(digest: string | undefined): string | null {
	const hash = digest?.split('@')[1] ?? digest;
	return hash ? hash.slice(0, hash.startsWith('sha256:') ? 19 : 12) : null;
}

/** What the networks add up to. */
export function networkTotals(networks: DockerNetwork[]) {
	return {
		count: networks.length,
		inUse: networks.filter((n) => n.containers.length > 0).length
	};
}

/** How a device's containers are doing, for the line at the top of the page. */
export function containerCounts(rows: DockerContainerRow[]) {
	return {
		total: rows.length,
		running: rows.filter((c) => c.state === 'running').length,
		unhealthy: rows.filter((c) => c.state === 'running' && c.health === 'unhealthy').length,
		crashed: rows.filter((c) => rowStatus(c) === 'offline').length
	};
}

/** What's different about a container, next to its name, and how much it matters. */
export interface RowNote {
	text: string;
	tone: 'amber' | 'red' | 'faint';
}

/** What a row says about a container that isn't simply up, or null if it is. */
export function rowNote(c: DockerContainerRow, now: number): RowNote | null {
	switch (c.state) {
		case 'running':
			return c.health === 'unhealthy' ? { text: 'Unhealthy', tone: 'amber' } : null;
		case 'restarting':
			return { text: 'Restarting', tone: 'amber' };
		case 'paused':
			return { text: 'Paused', tone: 'amber' };
		case 'dead':
			return { text: 'Dead', tone: 'red' };
		case 'exited': {
			if (rowStatus(c) !== 'offline') {
				return { text: `Stopped ${ago(c.finishedAt, now)}`, tone: 'faint' };
			}
			const why = c.oomKilled ? 'Out of memory' : `Exited ${c.exitCode}`;
			const again = c.restartCount > 0 ? `, restarted ${plural(c.restartCount, 'time')}` : '';
			return { text: `${why}${again}`, tone: 'red' };
		}
		default:
			return { text: c.status || c.state, tone: 'faint' };
	}
}

/** The port to show for a container: the first it publishes, and how many more. */
export function firstPort(ports: ContainerPort[]): { port: number; more: number } | null {
	const published = [...new Set(ports.flatMap((p) => (p.public == null ? [] : [p.public])))];
	return published.length ? { port: published[0], more: published.length - 1 } : null;
}
