// How the UI describes the tailnet: the path to each device, and how the hub
// reaches the internet.

import type { Device, Status, Uplink } from './api/types.js';
import { latency } from './format.js';

/** A round trip slow enough to point out, in milliseconds. */
export const slowRTT = 400;

/** The path to a device on the network map, such as "direct · 5.2 ms" or "via fra · 41 ms". */
export function routeLabel(d: Device): string {
	const p = d.path;
	const rtt = p.rttMs === undefined ? '' : ` · ${latency(p.rttMs)}`;
	switch (p.kind) {
		case 'self':
			return 'this hub';
		case 'direct':
			return `direct${rtt}`;
		case 'derp':
			return `via ${p.derp ?? 'relay'}${rtt}`;
		case 'peer-relay':
			return `via peer${rtt}`;
		case 'offline':
			return 'not observed';
	}
}

/** The path to a device in a table, such as "Direct" or "Relay fra". */
export function routeName(d: Device): string {
	const p = d.path;
	switch (p.kind) {
		case 'self':
			return 'This hub';
		case 'direct':
			return 'Direct';
		case 'derp':
			return p.derp ? `Relay ${p.derp}` : 'Relay';
		case 'peer-relay':
			return 'Peer relay';
		case 'offline':
			return 'Not observed';
	}
}

/**
 * Machines by how close they are to the hub: the shortest round trip first,
 * then the online ones with none yet, and then the offline ones. Machines
 * that tie stay in the order they came.
 */
export function nearestFirst(devices: Device[]): Device[] {
	const rank = (d: Device) => (!d.online ? Infinity : (d.path.rttMs ?? Number.MAX_VALUE));
	return devices.toSorted((a, b) => (rank(a) === rank(b) ? 0 : rank(a) - rank(b)));
}

/** Whether the link to a device is slow, or didn't answer its last ping. */
export function slowLink(d: Device): boolean {
	return d.online && (d.path.unanswered === true || (d.path.rttMs ?? 0) > slowRTT);
}

/** A device's IPv4 address on the tailnet, or else its first address. */
export function tailnetIP(d: Device): string | undefined {
	return d.ips.find((ip) => !ip.includes(':')) ?? d.ips[0];
}

/** Whether the hub reaches the internet, from the latest connectivity check. */
export function uplinkStatus(u: Uplink): Status {
	if (!u.checkedAt) {
		return 'unknown';
	}
	if (!u.ipv4 && !u.ipv6) {
		return 'offline';
	}
	// Without UDP, every connection is relayed.
	return u.udp ? 'online' : 'attention';
}

/** Whether the hub reaches one of Tailscale's DERP relays. */
export function derpStatus(u: Uplink): Status {
	if (u.derp) {
		return 'online';
	}
	return u.checkedAt ? 'offline' : 'unknown';
}
