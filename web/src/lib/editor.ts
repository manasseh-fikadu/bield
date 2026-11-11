// VS Code in the browser on a device: code-server, which Bield runs over SSH
// and serves on a port of its own for each device. Opening it in a tab is in
// editor/open.ts, which needs the page.

import type { DeviceEditor } from './api/types.js';

/**
 * Where to open the editor served at `base`, at `folder` if it's given.
 * Without one, code-server opens the folder it had open last.
 */
export function editorURL(base: string, folder?: string): string {
	if (!folder) {
		return base;
	}
	const url = new URL(base);
	url.searchParams.set('folder', folder);
	return url.href;
}

/**
 * Whether the device said its home folder has less free space than
 * installing code-server takes.
 */
export function lowOnSpace(e: Pick<DeviceEditor, 'disk' | 'free'>): boolean {
	return e.free !== undefined && e.disk !== undefined && e.free < e.disk;
}

/** Whether the device has an older code-server than the one Bield installs. */
export function canUpdate(e: Pick<DeviceEditor, 'version' | 'latest'>): boolean {
	return !!e.version && !!e.latest && e.version !== e.latest;
}
