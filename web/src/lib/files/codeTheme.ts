// How the editor looks. It reads the theme's CSS variables (layout.css), so the editor follows
// the rest of the page. Syntax is quiet too: names are bright, values are blue, and the rest is
// muted.

import { HighlightStyle, syntaxHighlighting } from '@codemirror/language';
import { EditorView } from '@codemirror/view';
import { tags as t } from '@lezer/highlight';

export const codeHighlight = syntaxHighlighting(
	HighlightStyle.define([
		{
			tag: [t.propertyName, t.definition(t.propertyName), t.keyword, t.heading, t.tagName],
			color: 'var(--color-fg)'
		},
		{
			tag: [t.string, t.number, t.bool, t.null, t.atom, t.url, t.regexp, t.special(t.string)],
			color: 'var(--color-blue)'
		},
		{ tag: [t.comment, t.meta], color: 'var(--color-fg-faint)' },
		{ tag: t.invalid, color: 'var(--color-red)' },
		{ tag: t.strong, fontWeight: '600' },
		{ tag: t.emphasis, fontStyle: 'italic' },
		{ tag: t.link, color: 'var(--color-blue)', textDecoration: 'underline' }
	])
);

/** YAML's plain values are content, which in other languages is ordinary text. */
export const yamlValues = syntaxHighlighting(
	HighlightStyle.define([{ tag: t.content, color: 'var(--color-blue)' }])
);

export const codeTheme = EditorView.theme(
	{
		'&': {
			height: '100%',
			fontSize: '12px',
			backgroundColor: 'var(--color-canvas)',
			color: 'var(--color-fg-muted)'
		},
		'&.cm-focused': { outline: 'none' },
		'.cm-scroller': { fontFamily: 'var(--font-mono)', lineHeight: '20px' },
		'.cm-content': { padding: '14px 0' },
		'.cm-gutters': {
			backgroundColor: 'var(--color-canvas)',
			color: 'var(--color-track)',
			border: 'none'
		},
		'.cm-lineNumbers .cm-gutterElement': { padding: '0 16px 0 16px' },
		'.cm-activeLineGutter': { backgroundColor: 'transparent', color: 'var(--color-fg-muted)' },
		'.cm-activeLine': { backgroundColor: 'rgb(255 255 255 / 0.03)' },
		'&.cm-focused > .cm-scroller > .cm-selectionLayer .cm-selectionBackground, .cm-selectionBackground, .cm-content ::selection':
			{ backgroundColor: 'rgb(127 169 245 / 0.28) !important' },
		'.cm-cursor, .cm-dropCursor': { borderLeftColor: 'var(--color-fg)' },
		'.cm-panels': { backgroundColor: 'var(--color-raised)', color: 'var(--color-fg)' },
		'.cm-panels.cm-panels-top': { borderBottom: '1px solid var(--color-line-strong)' },
		'.cm-panels.cm-panels-bottom': { borderTop: '1px solid var(--color-line-strong)' },
		'.cm-search': { fontFamily: 'var(--font-sans)', fontSize: '12.5px', padding: '6px 10px' },
		'.cm-textfield': {
			backgroundColor: 'var(--color-canvas)',
			border: '1px solid var(--color-line-strong)',
			borderRadius: '6px',
			color: 'var(--color-fg)',
			padding: '3px 8px'
		},
		'.cm-button': {
			backgroundImage: 'none',
			backgroundColor: 'var(--color-hover)',
			border: '1px solid var(--color-line-strong)',
			borderRadius: '6px',
			color: 'var(--color-fg)',
			padding: '3px 10px'
		},
		'.cm-searchMatch': {
			backgroundColor: 'rgb(245 181 68 / 0.22)',
			outline: '1px solid rgb(245 181 68 / 0.45)'
		},
		'.cm-searchMatch.cm-searchMatch-selected': { backgroundColor: 'rgb(245 181 68 / 0.45)' },
		'.cm-selectionMatch': { backgroundColor: 'rgb(127 169 245 / 0.14)' },
		'.cm-foldPlaceholder': {
			backgroundColor: 'var(--color-hover)',
			border: '1px solid var(--color-line-strong)',
			color: 'var(--color-fg-muted)'
		},
		'.cm-tooltip': {
			backgroundColor: 'var(--color-raised)',
			border: '1px solid var(--color-line-strong)'
		}
	},
	{ dark: true }
);
