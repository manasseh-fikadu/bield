import { describe, expect, test } from 'bun:test';
import { relayCommand, relayGrant, validPort } from './relay.js';

describe('relayCommand', () => {
	test('needs root on Linux and the BSDs, not on macOS or Windows', () => {
		expect(relayCommand('linux', 40000)).toBe('sudo tailscale set --relay-server-port=40000');
		expect(relayCommand('freebsd', 41000)).toBe('sudo tailscale set --relay-server-port=41000');
		expect(relayCommand('macOS', 40000)).toBe('tailscale set --relay-server-port=40000');
		expect(relayCommand('windows', 40000)).toBe('tailscale set --relay-server-port=40000');
	});
});

describe('relayGrant', () => {
	test('is a grant to paste into the policy file, as bield relay prints it', () => {
		expect(relayGrant(['100.64.0.4', '100.64.0.5'], '100.64.0.2')).toBe(
			'{\n  "src": ["100.64.0.4", "100.64.0.5"],\n  "dst": ["100.64.0.2"],\n  "app": { "tailscale.com/cap/relay": [] }\n}'
		);
	});
});

describe('validPort', () => {
	test('takes a whole number from 1 to 65535', () => {
		expect([1, 40000, 65535].map(validPort)).toEqual([true, true, true]);
		expect([0, 65536, 4.5, Number.NaN].map(validPort)).toEqual([false, false, false, false]);
	});
});
