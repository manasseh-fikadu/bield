import { describe, expect, test } from 'bun:test';
import {
	activeAlerts,
	alertedDevices,
	containerStatus,
	deviceIds,
	eventActor,
	groupContainers,
	mergeById,
	onDevice,
	openAlerts,
	parseAlertTab,
	quietlyOffline,
	troubleFirst,
	unitStatus
} from './alerts.js';
import type { Alert, AlertState, Container, Device, Event, MeasuredBy, Unit } from './api/types.js';
import type { Status } from './status.js';

function alert(id: number, state: AlertState, deviceId?: string): Alert {
	return {
		id,
		source: 'metrics',
		deviceId,
		severity: 'warning',
		state,
		title: `alert ${id}`,
		detail: '',
		openedAt: '2026-10-24T12:00:00Z',
		lastSeen: '2026-10-24T12:00:00Z',
		notes: []
	};
}

function device(id: string, measuredBy?: MeasuredBy): Device {
	return {
		id,
		name: id,
		hostName: id,
		dnsName: `${id}.example.ts.net`,
		os: 'linux',
		kind: 'server',
		ips: [],
		tags: [],
		online: true,
		status: 'online',
		statusReason: '',
		path: { kind: 'direct' },
		rxBytes: 0,
		txBytes: 0,
		measuredBy,
		docker: false,
		settings: {
			displayName: '',
			alertOffline: true,
			alertRelayed: false,
			thresholds: {},
			notes: ''
		}
	};
}

function container(name: string, change: Partial<Container> = {}): Container {
	return {
		id: name,
		name,
		image: 'img',
		state: 'running',
		status: '',
		exitCode: 0,
		restartCount: 0,
		restartPolicy: 'no',
		ports: [],
		deviceId: 'bield',
		checkIds: [],
		...change
	};
}

test('activeAlerts leaves out snoozed and resolved alerts', () => {
	const got = activeAlerts([
		alert(1, 'open'),
		alert(2, 'acknowledged'),
		alert(3, 'snoozed'),
		alert(4, 'resolved')
	]);
	expect(got.map((a) => a.id)).toEqual([1, 2]);
});

test.each([
	{ query: null, want: 'open' },
	{ query: 'snoozed', want: 'snoozed' },
	{ query: 'resolved', want: 'resolved' },
	{ query: 'other', want: 'open' }
])('parseAlertTab(%p)', ({ query, want }) => {
	expect(parseAlertTab(query)).toBe(want);
});

describe('onDevice', () => {
	// In embedded mode, Bield's node and the machine's own are both the hub.
	const devices = [device('bield', 'hub'), device('vps', 'hub'), device('laptop', 'agent')];
	const alerts = [alert(1, 'open', 'bield'), alert(2, 'open', 'laptop'), alert(3, 'open')];

	test.each<{ id: string; want: number[] }>([
		{ id: 'laptop', want: [2] },
		{ id: 'vps', want: [1] },
		{ id: 'bield', want: [1] }
	])("the $id's alerts", ({ id, want }) => {
		const d = devices.find((d) => d.id === id)!;
		expect(onDevice(alerts, d, devices).map((a) => a.id)).toEqual(want);
	});

	test('a device measured by no one is only itself', () => {
		expect([...deviceIds(device('phone'), devices)]).toEqual(['phone']);
	});
});

test.each<{ name: string; unit: Partial<Unit>; want: Status }>([
	{ name: 'active', unit: { active: 'active' }, want: 'online' },
	{ name: 'failed', unit: { active: 'failed' }, want: 'offline' },
	{ name: 'starting', unit: { active: 'activating' }, want: 'attention' },
	{ name: 'watched and inactive', unit: { active: 'inactive', watched: true }, want: 'attention' },
	{ name: 'inactive', unit: { active: 'inactive', watched: false }, want: 'unknown' }
])('unitStatus of a unit $name', ({ unit, want }) => {
	const u: Unit = {
		name: 'x.service',
		scope: 'system',
		description: '',
		active: 'active',
		sub: '',
		watched: true,
		deviceId: 'bield',
		...unit
	};
	expect(unitStatus(u)).toBe(want);
});

test.each<{ name: string; change: Partial<Container>; want: Status }>([
	{ name: 'running', change: {}, want: 'online' },
	{ name: 'unhealthy', change: { problem: 'is unhealthy' }, want: 'attention' },
	{
		name: 'stopped and meant to run',
		change: { state: 'exited', problem: 'stopped' },
		want: 'attention'
	},
	{ name: 'crashed', change: { state: 'exited', problem: 'crashed (exit 1)' }, want: 'offline' },
	{ name: 'done', change: { state: 'exited' }, want: 'unknown' }
])('containerStatus of a container $name', ({ change, want }) => {
	expect(containerStatus(container('x', change))).toBe(want);
});

test('groupContainers groups by project, and those in none last', () => {
	const groups = groupContainers([
		container('web-1', { project: 'web' }),
		container('loose'),
		container('db-1', { project: 'db' }),
		container('web-2', { project: 'web' })
	]);
	expect(groups.map((g) => [g.project, g.containers.map((c) => c.name)])).toEqual([
		['db', ['db-1']],
		['web', ['web-1', 'web-2']],
		[null, ['loose']]
	]);
});

test('mergeById puts newer events first, once each', () => {
	const event = (id: number): Event => ({
		id,
		at: '',
		level: 'info',
		kind: 'device',
		message: `${id}`
	});
	expect(mergeById([event(3), event(2)], [event(5), event(4), event(3)]).map((e) => e.id)).toEqual([
		5, 4, 3, 2
	]);
});

test('mergeById keeps older alerts loaded, and takes the fresh one of an ID', () => {
	const loaded = [alert(9, 'resolved'), alert(5, 'resolved'), alert(2, 'resolved')];
	const noted = { ...alert(5, 'resolved'), notes: [{ at: '', text: 'It was the logs' }] };
	const got = mergeById(loaded, [alert(12, 'resolved'), alert(9, 'resolved'), noted]);
	expect(got.map((a) => [a.id, a.notes.length])).toEqual([
		[12, 0],
		[9, 0],
		[5, 1],
		[2, 0]
	]);
});

test('openAlerts leaves out what someone has seen', () => {
	const got = openAlerts([alert(1, 'open'), alert(2, 'acknowledged'), alert(3, 'snoozed')]);
	expect(got.map((a) => a.id)).toEqual([1]);
});

test('alertedDevices has the devices with an alert that has not resolved', () => {
	const got = alertedDevices([
		alert(1, 'open', 'a'),
		alert(2, 'snoozed', 'b'),
		alert(3, 'resolved', 'c'),
		alert(4, 'open')
	]);
	expect([...got].sort()).toEqual(['a', 'b']);
});

describe('troubleFirst', () => {
	const off = (id: string): Device => ({ ...device(id), online: false, status: 'offline' });
	const warn = (id: string): Device => ({ ...device(id), status: 'attention' });

	test('sorts by what needs someone, keeping the order within each', () => {
		const got = troubleFirst(
			[off('tablet'), device('hub'), warn('nas'), off('pi'), device('vps')],
			new Set(['pi'])
		);
		expect(got.map((d) => d.id)).toEqual(['pi', 'nas', 'hub', 'vps', 'tablet']);
	});

	test('a device that is off with no alert is quiet', () => {
		expect(quietlyOffline(off('a'), new Set())).toBe(true);
		expect(quietlyOffline(off('a'), new Set(['a']))).toBe(false);
		expect(quietlyOffline(device('a'), new Set())).toBe(false);
	});
});

describe('eventActor', () => {
	const cases: { e: { actor?: string; token?: string; via?: string }; want: string }[] = [
		{ e: {}, want: '' },
		{ e: { token: 'deploy' }, want: '' },
		{ e: { actor: 'alice' }, want: 'by alice' },
		{ e: { actor: 'alice', via: 'cli' }, want: 'by alice with the CLI' },
		{ e: { actor: 'alice', via: 'mcp' }, want: 'by alice with MCP' },
		{ e: { actor: 'alice', token: 'deploy' }, want: 'by alice with the token deploy' },
		{
			e: { actor: 'alice', token: 'deploy', via: 'cli' },
			want: 'by alice with the token deploy, with the CLI'
		}
	];
	for (const c of cases) {
		test(c.want || 'nobody', () => expect(eventActor(c.e)).toBe(c.want));
	}
});
