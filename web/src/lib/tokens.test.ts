import { describe, expect, test } from 'bun:test';
import { RoleAdmin, RoleViewer, type APIToken } from './api/types.js';
import { actsAsNote, sentence, tokenHintLabel, tokenNameProblem } from './tokens.js';

describe('tokenNameProblem', () => {
	const cases: { name: string; taken?: string[]; ok: boolean }[] = [
		{ name: 'deploy', ok: true },
		{ name: ' ci.main_2-x ', ok: true },
		{ name: '9lives', ok: true },
		{ name: 'a'.repeat(40), ok: true },
		{ name: '', ok: false },
		{ name: '   ', ok: false },
		{ name: 'a'.repeat(41), ok: false },
		{ name: '.hidden', ok: false },
		{ name: '-flag', ok: false },
		{ name: 'has space', ok: false },
		{ name: 'émoji', ok: false },
		{ name: 'Deploy', taken: ['deploy'], ok: false },
		{ name: 'deploy2', taken: ['deploy'], ok: true }
	];
	for (const c of cases) {
		test(JSON.stringify(c.name), () => {
			expect(tokenNameProblem(c.name, c.taken) === '').toBe(c.ok);
		});
	}
});

test('tokenHintLabel', () => {
	expect(tokenHintLabel('AB12')).toBe('bield_…AB12');
});

describe('actsAsNote', () => {
	const base: APIToken = {
		id: 1,
		name: 'deploy',
		hint: 'AB12',
		role: RoleAdmin,
		actsAs: RoleAdmin,
		createdBy: 'alice@example.com',
		createdAt: '2026-11-01T00:00:00Z',
		expiresAt: '2026-12-01T00:00:00Z',
		expired: false
	};
	test('full role', () => expect(actsAsNote(base)).toBe(''));
	test('maker demoted', () =>
		expect(actsAsNote({ ...base, actsAs: RoleViewer })).toContain('viewer'));
	test('maker has no role', () =>
		expect(actsAsNote({ ...base, actsAs: undefined })).toContain('no role'));
	test('expired', () => expect(actsAsNote({ ...base, actsAs: undefined, expired: true })).toBe(''));
	test('revoked', () =>
		expect(actsAsNote({ ...base, actsAs: undefined, revokedAt: '2026-11-11T00:00:00Z' })).toBe(''));
});

test('sentence', () => {
	expect(sentence('no such API token')).toBe('No such API token.');
	expect(sentence('Already one.')).toBe('Already one.');
	expect(sentence('')).toBe('');
});
