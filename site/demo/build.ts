// Builds the clickable demo into public/demo, for the landing page to show: the real dashboard,
// built to live under a path, with what `bield serve --demo` answers recorded next to it, and a
// stand-in for the hub that replays that. `make site-demo` runs it.
//
// BIELD_DEMO_BASE is the path it'll be served from, which is /demo unless the page lives under a
// path of its own, as it does on GitHub Pages (/bield/demo). BIELD_BIN is the binary to record.

import { spawn } from 'node:child_process';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
import { join, resolve } from 'node:path';
import { record } from './record.js';

const site = resolve(import.meta.dirname, '..');
const out = join(site, 'public/demo');
const base = process.env.BIELD_DEMO_BASE ?? '/demo';
const bin = resolve(process.env.BIELD_BIN ?? join(site, '../bield'));
const listen = '127.0.0.1:4703';

function run(command: string, args: string[], cwd: string, env: Record<string, string> = {}) {
	return new Promise<void>((done, fail) => {
		const child = spawn(command, args, {
			cwd,
			stdio: 'inherit',
			env: { ...process.env, ...env }
		});
		child.on('exit', (code) =>
			code === 0 ? done() : fail(new Error(`${command} ${args.join(' ')} failed`))
		);
	});
}

console.log(`Building the dashboard for ${base}`);
await run('bun', ['run', 'build'], join(site, '../web'), { BIELD_BASE: base, BIELD_OUT: out });

const built = await Bun.build({
	entrypoints: [join(site, 'demo/shim.ts')],
	target: 'browser',
	format: 'iife',
	minify: true
});
if (!built.success) {
	throw new AggregateError(built.logs, 'Building the shim failed');
}
await writeFile(join(out, 'shim.js'), await built.outputs[0].text());

console.log('Recording the demo');
const hub = spawn(bin, ['serve', '--demo', '--demo-listen', listen], { stdio: 'ignore' });
let recording;
try {
	const url = `http://${listen}`;
	for (let up = false, tries = 0; !up; tries++) {
		if (tries > 120) {
			throw new Error("The demo didn't start");
		}
		await Bun.sleep(500);
		up = await fetch(`${url}/api/health`).then(
			(r) => r.ok,
			() => false
		);
	}
	recording = await record(url, out);
} finally {
	hub.kill();
}

// A static host has no fallback to the app for /docker and the rest, so each gets a copy of it,
// with the stand-in loaded first. It's a short page: the app is in the files it loads.
const app = (await readFile(join(out, '200.html'), 'utf8')).replace(
	'<head>',
	`<head><script src="${base}/shim.js"></script>`
);
const pages = [
	'',
	'devices',
	'devices/agents',
	'alerts',
	'checks',
	'docker',
	'terminal',
	'files',
	'activity',
	'settings',
	'setup',
	...recording.deviceIds.map((id) => `devices/${id}`),
	...recording.containerNames.map((name) => `docker/${name}`)
];
for (const page of pages) {
	await mkdir(join(out, page), { recursive: true });
	await writeFile(join(out, page, 'index.html'), app);
}
await writeFile(join(out, '200.html'), app);
console.log(`Done: ${pages.length} pages in ${out}`);
