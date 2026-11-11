// API tokens, for the Settings tab that lists, makes and revokes them. The
// hub checks everything; these helpers only decide what the form says.

import { DefaultTokenDays, MaxTokenDays, RoleViewer, type APIToken } from './api/types.js';

/** The expiry choices the make dialog offers, in days. */
export const tokenDayOptions: readonly number[] = [7, DefaultTokenDays, 90, MaxTokenDays];

/** The longest name a token can have. It matches tokenName in internal/api/tokens.go. */
export const maxTokenNameLength = 40;

const tokenName = /^[A-Za-z0-9][A-Za-z0-9._-]*$/;

/**
 * What is wrong with a token name typed into the make dialog, in a sentence,
 * or '' if nothing is. `taken` are the names of the tokens there are, which
 * the hub compares without case.
 */
export function tokenNameProblem(name: string, taken: readonly string[] = []): string {
	const s = name.trim();
	if (s === '') {
		return 'Give the token a name.';
	}
	if (s.length > maxTokenNameLength) {
		return `A name has at most ${maxTokenNameLength} characters.`;
	}
	if (!tokenName.test(s)) {
		return 'Use letters, digits, dots, dashes and underscores, starting with a letter or digit.';
	}
	const lower = s.toLowerCase();
	if (taken.some((t) => t.toLowerCase() === lower)) {
		return `Another token is called ${s}. Pick another name, or revoke that one first.`;
	}
	return '';
}

/** How a token is shown without its secret, such as bield_…AB12. */
export function tokenHintLabel(hint: string): string {
	return `bield_…${hint}`;
}

/**
 * A sentence for a token that can do less than its role, or '' if it can do
 * all of it. An expired token is marked as such elsewhere, so this says
 * nothing about it.
 */
export function actsAsNote(token: APIToken): string {
	if (token.expired || token.actsAs === token.role) {
		return '';
	}
	if (!token.actsAs) {
		return 'Does nothing: its maker has no role now.';
	}
	return token.actsAs === RoleViewer ? 'Acts as a viewer: its maker is a viewer now.' : '';
}

/** A message from the hub as a sentence, since its errors start in lower case. */
export function sentence(message: string): string {
	const s = message.trim();
	if (s === '') {
		return s;
	}
	const capital = s[0].toUpperCase() + s.slice(1);
	return /[.!?]$/.test(capital) ? capital : `${capital}.`;
}
