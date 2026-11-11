import { describe, expect, test } from 'bun:test';
import type { Device, Path, Status, Uplink } from './api/types.js';
import {
	derpStatus,
	nearestFirst,
	routeLabel,
	routeName,
	slowLink,
	tailnetIP,
	uplinkStatus
} from './tailnet.js';

function device(path: Path, online = true, ips = ['100.64.0.2', 'fd7a:115c:a1e0::2']): Device {
	return {
		id: 'laptop',
		name: 'laptop',
		hostName: 'laptop',
		dnsName: 'laptop.example.ts.net',
		os: 'macOS',
		kind: 'laptop',
		ips,
		tags: [],
		online,
		status: online ? 'online' : 'offline',
		statusReason: '',
		path,
		rxBytes: 0,
		txBytes: 0,
		docker: false,
		settings: { displayName: '', alertOffline: true, thresholds: {}, notes: '' }
	};
}

describe('routeLabel and routeName', () => {
	test.each<{ name: string; path: Path; label: string; route: string }>([
		{ name: 'the hub', path: { kind: 'self' }, label: 'this hub', route: 'This hub' },
		{
			name: 'direct with a round trip',
			path: { kind: 'direct', rttMs: 5.24 },
			label: 'direct · 5.2 ms',
			route: 'Direct'
		},
		{ name: 'direct with none', path: { kind: 'direct' }, label: 'direct', route: 'Direct' },
		{
			name: 'through a relay',
			path: { kind: 'derp', derp: 'fra', rttMs: 41 },
			label: 'via fra · 41 ms',
			route: 'Relay fra'
		},
		{ name: 'an unnamed relay', path: { kind: 'derp' }, label: 'via relay', route: 'Relay' },
		{
			name: 'through a peer',
			path: { kind: 'peer-relay', rttMs: 12 },
			label: 'via peer · 12 ms',
			route: 'Peer relay'
		},
		{ name: 'offline', path: { kind: 'offline' }, label: 'not observed', route: 'Not observed' }
	])('$name', ({ path, label, route }) => {
		expect(routeLabel(device(path))).toBe(label);
		expect(routeName(device(path))).toBe(route);
	});
});

test('nearestFirst puts the shortest trip first, then no trip yet, then offline', () => {
	const named = (id: string, path: Path, online = true) => ({ ...device(path, online), id });
	const got = nearestFirst([
		named('off', { kind: 'offline' }, false),
		named('none', { kind: 'direct' }),
		named('far', { kind: 'derp', rttMs: 90 }),
		named('off2', { kind: 'offline', rttMs: 1 }, false),
		named('near', { kind: 'direct', rttMs: 3 })
	]);
	expect(got.map((d) => d.id)).toEqual(['near', 'far', 'none', 'off', 'off2']);
});

describe('slowLink', () => {
	test.each<{ name: string; path: Path; online?: boolean; want: boolean }>([
		{ name: 'fast', path: { kind: 'direct', rttMs: 20 }, want: false },
		{ name: 'slow', path: { kind: 'derp', rttMs: 480 }, want: true },
		{ name: 'unanswered', path: { kind: 'direct', unanswered: true }, want: true },
		{
			name: 'offline isn’t slow',
			path: { kind: 'offline', rttMs: 900 },
			online: false,
			want: false
		}
	])('$name', ({ path, online, want }) => {
		expect(slowLink(device(path, online))).toBe(want);
	});
});

describe('tailnetIP', () => {
	test.each<{ name: string; ips: string[]; want: string | undefined }>([
		{ name: 'IPv4 first', ips: ['fd7a:115c:a1e0::2', '100.64.0.2'], want: '100.64.0.2' },
		{ name: 'IPv6 if that’s all', ips: ['fd7a:115c:a1e0::2'], want: 'fd7a:115c:a1e0::2' },
		{ name: 'none', ips: [], want: undefined }
	])('$name', ({ ips, want }) => {
		expect(tailnetIP(device({ kind: 'direct' }, true, ips))).toBe(want);
	});
});

describe('uplink', () => {
	const checkedAt = '2026-10-22T12:00:00Z';
	test.each<{ name: string; uplink: Uplink; internet: Status; derp: Status }>([
		{
			name: 'not checked yet',
			uplink: { udp: false, ipv4: false, ipv6: false },
			internet: 'unknown',
			derp: 'unknown'
		},
		{
			name: 'all good',
			uplink: {
				checkedAt,
				udp: true,
				ipv4: true,
				ipv6: false,
				derp: { code: 'fra', name: 'Frankfurt', latencyMs: 3.8 }
			},
			internet: 'online',
			derp: 'online'
		},
		{
			name: 'UDP blocked',
			uplink: { checkedAt, udp: false, ipv4: true, ipv6: true },
			internet: 'attention',
			derp: 'offline'
		},
		{
			name: 'no internet',
			uplink: { checkedAt, udp: false, ipv4: false, ipv6: false },
			internet: 'offline',
			derp: 'offline'
		}
	])('$name', ({ uplink, internet, derp }) => {
		expect(uplinkStatus(uplink)).toBe(internet);
		expect(derpStatus(uplink)).toBe(derp);
	});
});
