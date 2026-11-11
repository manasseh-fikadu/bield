import { describe, expect, test } from 'bun:test';
import type { Device, Thresholds } from './api/types.js';
import { levelText, limitsOf, tagCounts, tagName } from './device.js';

const hub = { cpuWarn: 90, memoryWarn: 80, diskWarn: 85, diskCritical: 95 } as Thresholds;

describe('limitsOf', () => {
	test('takes the hub limits where the device has none of its own', () => {
		const d = { settings: { thresholds: { diskWarn: 70 } } } as Device;
		expect(limitsOf(d, hub)).toEqual({ cpu: 90, memory: 80, disk: 70, diskCritical: 95 });
	});
});

describe('levelText', () => {
	test.each([
		['under the limit', 50, 'text-fg'],
		['at the warning', 85, 'text-amber'],
		['at the critical level', 95, 'text-red-fg'],
		['unknown', null, 'text-fg-faint']
	])('%s', (_name, value, want) => {
		expect(levelText(value, 85, 95)).toBe(want);
	});

	test('has no red without a critical level, and can say what fine looks like', () => {
		expect(levelText(99, 85)).toBe('text-amber');
		expect(levelText(10, 85, 95, 'text-fg-muted')).toBe('text-fg-muted');
	});
});

describe('tagCounts', () => {
	test('lists every tag once, in order, with how many devices have it', () => {
		const devices = [
			{ tags: ['tag:server', 'tag:backup'] },
			{ tags: [] },
			{ tags: ['tag:server'] },
			{ tags: ['tag:prod', 'tag:server', 'tag:prod'] }
		] as unknown as Device[];
		expect(tagCounts(devices)).toEqual([
			{ tag: 'tag:backup', count: 1 },
			{ tag: 'tag:prod', count: 1 },
			{ tag: 'tag:server', count: 3 }
		]);
	});

	test('is empty when no device has a tag', () => {
		expect(tagCounts([{ tags: [] }] as unknown as Device[])).toEqual([]);
	});
});

describe('tagName', () => {
	test.each([
		['tag:server', 'server'],
		['server', 'server'],
		['tag:tag:odd', 'tag:odd']
	])('%s', (tag, want) => {
		expect(tagName(tag)).toBe(want);
	});
});
