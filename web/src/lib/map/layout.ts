// Where the network map puts its cards and the lines between them: a tree
// from the top down, with the internet and the home relay over the hub, and
// the machines under it in rows, nearest first. The grid has two halves, and
// one trunk runs down the gutter between them. Each row hangs from a bus off
// the trunk, so no line crosses a card.

export interface Box {
	x: number;
	y: number;
	w: number;
	h: number;
}

/** The line from a row's bus down to one card. */
export interface Drop {
	id: string;
	/** From the bus to the card, with the bus's rounded corner at the ends of a row. */
	d: string;
	/** From the hub, down the trunk and along the bus to the card, for when it's lit. */
	lit: string;
	/** Where the line's label starts, beside the line, and its top. */
	lx: number;
	ly: number;
}

export interface Layout {
	width: number;
	height: number;
	/** How many cards a row has: always even, so the trunk runs between two. */
	cols: number;
	/** The x of the trunk, in the middle. */
	trunk: number;
	internet: Box;
	relay: Box;
	hub: Box;
	cards: Map<string, Box>;
	/** The slot after the last machine, for what the lines mean. */
	legend: Box;
	/** The lines every card shares: the trunk, and each row's bus. */
	shared: string;
	/** The lines from the internet and the relay down to where they meet over the hub. */
	internetLine: string;
	relayLine: string;
	/** Where the labels of those two lines start. */
	internetLabel: { x: number; y: number };
	relayLabel: { x: number; y: number };
	drops: Drop[];
}

export const cardH = 92;
export const minCard = 196;
export const maxCard = 240;
/** Between cards in a half of the grid. */
export const gap = 14;
/** Between the two halves, where the trunk runs. */
export const gutter = 30;
export const hubW = 438;
export const hubH = 92;
const upstreamH = 64;
/** The room the title needs beside the upstream cards, and its height above them. */
const titleW = 218;
const titleH = 44;
/** From a card's bottom to the bus of the row under it, and from the bus to that row. */
const underCard = 16;
const overCard = 26;
/** The radius of the corners where a bus turns down. */
const corner = 6;

/** How many columns fit in a map width pixels wide, or 0 if even two don't. */
export function columns(width: number): number {
	for (const cols of [6, 4, 2]) {
		if (cols * minCard + (cols - 2) * gap + gutter <= width) {
			return cols;
		}
	}
	return 0;
}

/** Lays out the map for machines in the order of `ids`, or returns null if it's too narrow. */
export function layoutMap(width: number, ids: string[]): Layout | null {
	const cols = columns(width);
	if (!cols) {
		return null;
	}
	const half = cols / 2;
	const w = Math.min(maxCard, (width - (cols - 2) * gap - gutter) / cols);
	const grid = cols * w + (cols - 2) * gap + gutter;
	const x0 = (width - grid) / 2;
	const trunk = width / 2;
	const colX = (i: number) => x0 + i * (w + gap) + (i >= half ? gutter - gap : 0);

	const titleBeside = trunk - gutter / 2 - w >= titleW;
	const top = (titleBeside ? 0 : titleH) + 6;
	const internet = { x: trunk - gutter / 2 - w, y: top, w, h: upstreamH };
	const relay = { x: trunk + gutter / 2, y: top, w, h: upstreamH };
	const upBottom = top + upstreamH;
	const join = upBottom + 22;
	const hw = Math.min(hubW, grid);
	const hub = { x: trunk - hw / 2, y: join + 18, w: hw, h: hubH };
	const hubBottom = hub.y + hub.h;

	const ix = internet.x + w / 2;
	const rx = relay.x + w / 2;
	const internetLine = `M${ix} ${upBottom}V${join - corner}Q${ix} ${join} ${ix + corner} ${join}H${trunk}`;
	const relayLine = `M${trunk} ${join}H${rx - corner}Q${rx} ${join} ${rx} ${join - corner}V${upBottom}`;
	let shared = `M${trunk} ${join}V${hub.y}`;

	const cards = new Map<string, Box>();
	const drops: Drop[] = [];
	let cardTop = hubBottom + 20 + overCard;
	let lastBus = 0;
	for (let r = 0; r * cols < ids.length; r++) {
		const row = ids.slice(r * cols, (r + 1) * cols);
		const bus = cardTop - overCard;
		lastBus = bus;
		const centers = row.map((_, i) => colX(i) + w / 2);
		const first = centers[0];
		const last = centers[centers.length - 1];
		const start = first < trunk ? first + corner : trunk;
		const end = last > trunk ? last - corner : trunk;
		shared += `M${start} ${bus}H${end}`;
		row.forEach((id, i) => {
			const cx = centers[i];
			cards.set(id, { x: colX(i), y: cardTop, w, h: cardH });
			// A card at either end of the row turns off the bus at a corner.
			const end = (i === 0 && cx < trunk) || (i === row.length - 1 && cx > trunk);
			const side = cx < trunk ? corner : -corner;
			const turn = `Q${cx} ${bus} ${cx} ${bus + corner}V${cardTop}`;
			drops.push({
				id,
				d: end ? `M${cx + side} ${bus}${turn}` : `M${cx} ${bus}V${cardTop}`,
				lit: `M${trunk} ${hubBottom}V${bus}H${cx + side}${turn}`,
				lx: cx + 8,
				ly: bus + 6
			});
		});
		cardTop += cardH + underCard + overCard;
	}
	if (ids.length) {
		shared += `M${trunk} ${hubBottom}V${lastBus}`;
	}

	// The legend takes the slot after the last machine, or starts a row of its own.
	const used = ids.length % cols;
	const lastTop = cardTop - (cardH + underCard + overCard);
	const legend =
		ids.length && used
			? { x: colX(used), y: lastTop, w, h: cardH }
			: { x: colX(0), y: ids.length ? lastTop + cardH + underCard : hubBottom + 20, w, h: cardH };

	return {
		width,
		height: Math.max(legend.y + legend.h, ids.length ? lastTop + cardH : 0),
		cols,
		trunk,
		internet,
		relay,
		hub,
		cards,
		legend,
		shared,
		internetLine,
		relayLine,
		internetLabel: { x: ix + 8, y: upBottom + 4 },
		relayLabel: { x: rx + 8, y: upBottom + 4 },
		drops
	};
}
