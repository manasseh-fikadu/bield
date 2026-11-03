// How many containers run on each machine with Docker, for Overview. The
// snapshot only has the hub's, so this asks each machine, once a minute.

import type { Device } from '#lib/api/types.js';
import { getDockerContainers } from './api.js';

export interface ContainerCount {
	running: number;
	total: number;
}

/** How many machines to ask at once. Asking a machine can mean opening SSH to it. */
const atOnce = 4;
const interval = 60_000;

export class ContainerCounts {
	/** What each machine last said, by device ID. A machine that never answered isn't in it. */
	byDevice = $state.raw(new Map<string, ContainerCount>());

	/**
	 * Starts asking the machines with these device IDs, and returns what stops
	 * it. Call it in an `$effect`.
	 */
	start(ids: string[]): () => void {
		let current = true;

		const askOne = async (id: string) => {
			try {
				const { containers } = await getDockerContainers(id);
				if (!current) {
					return;
				}
				const running = containers.filter((c) => c.state === 'running').length;
				this.byDevice = new Map(this.byDevice).set(id, { running, total: containers.length });
			} catch {
				// A machine that doesn't answer keeps what it last said.
			}
		};

		const load = async () => {
			const queue = [...ids];
			await Promise.all(
				Array.from({ length: Math.min(atOnce, queue.length) }, async () => {
					for (let id = queue.shift(); id !== undefined && current; id = queue.shift()) {
						await askOne(id);
					}
				})
			);
		};

		void load();
		const timer = setInterval(load, interval);
		return () => {
			current = false;
			clearInterval(timer);
		};
	}

	/** The containers on the machines in `devices`, or null if none has answered. */
	total(devices: Device[]): ContainerCount | null {
		const counts = devices.flatMap((d) => this.byDevice.get(d.id) ?? []);
		return counts.length
			? {
					running: counts.reduce((n, c) => n + c.running, 0),
					total: counts.reduce((n, c) => n + c.total, 0)
				}
			: null;
	}
}
