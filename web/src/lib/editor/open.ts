// Opening VS Code on a device. Its editor is served on a port of Bield's own,
// another origin, so it gets a tab of its own instead of a frame.

import { startEditor } from '#lib/api/client.js';
import { editorURL } from '#lib/editor.js';
import { toast } from '#lib/toast.svelte.js';

/**
 * Opens VS Code on a device in a new tab, at `folder` if it's given, or else
 * at the folder it had open last, and starts it first unless it runs. Call
 * it from a click. The tab opens before the start, while the browser still
 * counts it as the click's: Safari blocks a tab opened after an await. It
 * resolves once the tab is on its way, or the start failed.
 */
export async function openEditor(deviceId: string, folder?: string): Promise<void> {
	const tab = window.open('about:blank', '_blank');
	if (tab) {
		// The editor's page can't reach back into Bield's through window.opener.
		tab.opener = null;
		tab.document.title = 'Starting VS Code…';
		tab.document.body.textContent = 'Starting VS Code…';
	}
	let url: string;
	try {
		({ url } = await startEditor(deviceId));
	} catch {
		// The client showed the error as a toast.
		tab?.close();
		return;
	}
	const target = editorURL(url, folder);
	if (tab) {
		tab.location.href = target;
		return;
	}
	// The browser blocked the tab. A click on the toast is a click of its own,
	// which it lets open one.
	toast('VS Code is running. Your browser blocked its tab.', 'default', {
		action: { label: 'Open', run: () => window.open(target, '_blank', 'noopener') },
		duration: 15_000
	});
}
