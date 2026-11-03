import { describe, expect, test } from 'bun:test';
import { cardH, columns, gap, gutter, layoutMap, maxCard, minCard } from './layout.js';

function ids(n: number): string[] {
	return Array.from({ length: n }, (_, i) => `m${i}`);
}

describe('columns', () => {
	test.each([
		[400, 0],
		[2 * minCard + gutter, 2],
		[874, 4],
		[1261, 4],
		[6 * minCard + 4 * gap + gutter, 6],
		[3000, 6]
	])('a map %p wide has %p columns', (width, want) => {
		expect(columns(width)).toBe(want);
	});
});

describe('layoutMap', () => {
	test('is null when two cards do not fit', () => {
		expect(layoutMap(300, ids(3))).toBeNull();
	});

	test('lays out the design at 874 wide: 204 cards, the trunk in the middle', () => {
		const l = layoutMap(874, ids(11))!;
		expect(l.cols).toBe(4);
		expect(l.trunk).toBe(437);
		expect(l.internet.y).toBe(6);
		expect([...l.cards.values()].map((b) => b.x).slice(0, 4)).toEqual([0, 218, 452, 670]);
		expect(l.cards.get('m0')!.w).toBe(204);
		expect(l.hub.x + l.hub.w / 2).toBe(l.trunk);
		expect(l.internet.x + l.internet.w + gutter / 2).toBe(l.trunk);
		expect(l.relay.x - gutter / 2).toBe(l.trunk);
	});

	test('keeps every card clear of the trunk', () => {
		for (const width of [430, 700, 874, 1300, 2000]) {
			const l = layoutMap(width, ids(9))!;
			for (const b of l.cards.values()) {
				expect(b.x + b.w <= l.trunk || b.x >= l.trunk).toBe(true);
			}
		}
	});

	test('caps the width of a card', () => {
		const l = layoutMap(1100, ids(4))!;
		expect(l.cols).toBe(4);
		expect(l.cards.get('m0')!.w).toBe(maxCard);
	});

	test('fills rows in order, nearest first', () => {
		const l = layoutMap(874, ids(6))!;
		const [first, endOfRow, nextRow, under] = ['m0', 'm3', 'm4', 'm5'].map((id) =>
			l.cards.get(id)!
		);
		expect(endOfRow.y).toBe(first.y);
		expect(nextRow.y).toBe(first.y + cardH + 42);
		expect(nextRow.x).toBe(first.x);
		expect(under.x).toBe(l.cards.get('m1')!.x);
	});

	test('puts the legend in the slot after the last machine, or a row of its own', () => {
		const partial = layoutMap(874, ids(3))!;
		expect(partial.legend.x).toBe(670);
		expect(partial.legend.y).toBe(partial.cards.get('m0')!.y);
		const full = layoutMap(874, ids(4))!;
		expect(full.legend.x).toBe(0);
		expect(full.legend.y).toBeGreaterThan(full.cards.get('m0')!.y);
		expect(full.height).toBe(full.legend.y + full.legend.h);
	});

	test('turns the corners at the ends of a row, and drops straight in the middle', () => {
		const l = layoutMap(874, ids(4))!;
		const drop = (id: string) => l.drops.find((d) => d.id === id)!.d;
		expect(drop('m0')).toContain('Q');
		expect(drop('m1')).not.toContain('Q');
		expect(drop('m2')).not.toContain('Q');
		expect(drop('m3')).toContain('Q');
	});

	test('runs the bus to the trunk when a row has cards on one side only', () => {
		const l = layoutMap(874, ids(5))!;
		// m4 is alone on the last row, left of the trunk, so the bus runs from it to the trunk.
		const lastBus = l.cards.get('m4')!.y - 26;
		expect(l.shared).toContain(`M${102 + 6} ${lastBus}H${l.trunk}`);
		expect(l.shared).toContain(`V${lastBus}`);
	});

	test('lights a line from the hub to the card', () => {
		const l = layoutMap(874, ids(2))!;
		const hubBottom = l.hub.y + l.hub.h;
		const lit = l.drops.find((d) => d.id === 'm1')!.lit;
		expect(lit.startsWith(`M${l.trunk} ${hubBottom}V`)).toBe(true);
		expect(lit.endsWith(`V${l.cards.get('m1')!.y}`)).toBe(true);
	});

	test('puts the title above the upstream cards when it does not fit beside them', () => {
		const l = layoutMap(500, ids(2))!;
		expect(l.internet.y).toBeGreaterThan(40);
	});
});
