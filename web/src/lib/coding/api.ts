// The coding routes: what the coding tools on the tailnet's machines used,
// for every machine or for one, and the admin's switch that turns watching a
// machine's tools on or off.

import { mutate, request } from '#lib/api/client.js';
import type { CodingSummary, SetCoding, WatchedCoding } from '#lib/api/types.js';
import { browserTimeZone, type CodingRange } from './coding.js';

/** What every machine's coding tools used over a range, with the days in the browser's time zone. */
export function getCoding(range: CodingRange): Promise<CodingSummary> {
	return request('GET', `coding?range=${range}&tz=${encodeURIComponent(browserTimeZone())}`);
}

/** What one machine's coding tools used. */
export function getDeviceCoding(id: string, range: CodingRange): Promise<CodingSummary> {
	return request(
		'GET',
		`devices/${encodeURIComponent(id)}/coding?range=${range}&tz=${encodeURIComponent(browserTimeZone())}`
	);
}

/** Turns watching a machine's coding tools on or off. Admins only. */
export function setDeviceCoding(
	id: string,
	enabled: boolean,
	success?: string
): Promise<SetCoding> {
	return mutate('PUT', `devices/${encodeURIComponent(id)}/coding`, { enabled }, { success });
}

/**
 * Turns watching the AI tools on for every machine that has an agent, or off
 * for every machine. Admins only.
 */
export function watchAllCoding(enabled: boolean): Promise<WatchedCoding> {
	return mutate('POST', 'coding/watch', { enabled });
}
