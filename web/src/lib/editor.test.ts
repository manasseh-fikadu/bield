import { describe, expect, test } from 'bun:test';
import { canUpdate, editorURL, lowOnSpace } from './editor.js';

describe('editorURL', () => {
	test.each([
		[
			'no folder',
			'https://bield.example.ts.net:4710',
			undefined,
			'https://bield.example.ts.net:4710'
		],
		[
			'an empty folder',
			'https://bield.example.ts.net:4710/',
			'',
			'https://bield.example.ts.net:4710/'
		],
		[
			'a folder',
			'https://bield.example.ts.net:4710',
			'/home/alice',
			'https://bield.example.ts.net:4710/?folder=%2Fhome%2Falice'
		],
		[
			'a trailing slash',
			'https://bield.example.ts.net:4710/',
			'/home/alice',
			'https://bield.example.ts.net:4710/?folder=%2Fhome%2Falice'
		],
		[
			'spaces and symbols',
			'https://bield.example.ts.net:4710/',
			'/home/alice/my project & co',
			'https://bield.example.ts.net:4710/?folder=%2Fhome%2Falice%2Fmy+project+%26+co'
		],
		[
			'a query already',
			'http://100.64.0.1:4711/?a=1',
			'/srv',
			'http://100.64.0.1:4711/?a=1&folder=%2Fsrv'
		]
	])('%s', (_, base, folder, want) => {
		expect(editorURL(base, folder)).toBe(want);
	});

	test('the folder comes back as it was', () => {
		const folder = '/home/alice/my project/ü #1';
		const url = new URL(editorURL('https://bield.example.ts.net:4710', folder));
		expect(url.searchParams.get('folder')).toBe(folder);
	});
});

describe('lowOnSpace', () => {
	test.each([
		['unknown free space', { disk: 600 }, false],
		['unknown size', { free: 100 }, false],
		['enough', { disk: 600, free: 600 }, false],
		['too little', { disk: 600, free: 599 }, true]
	])('%s', (_, e, want) => {
		expect(lowOnSpace(e)).toBe(want);
	});
});

describe('canUpdate', () => {
	test.each([
		['the latest', { version: '4.105.1', latest: '4.105.1' }, false],
		['an older one', { version: '4.104.0', latest: '4.105.1' }, true],
		['no version yet', { latest: '4.105.1' }, false]
	])('%s', (_, e, want) => {
		expect(canUpdate(e)).toBe(want);
	});
});
