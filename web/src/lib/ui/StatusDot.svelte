<!--
	A lamp in the color of a status: lit while online, amber or red when
	something's wrong, and a ring while off. It's decoration: say the status
	in words too.
-->
<script lang="ts">
	import { statusMeta, type Status } from '#lib/status.js';

	interface Props {
		status: Status;
		/**
		 * Breathes, for an alert nobody acknowledged yet. Only amber and red
		 * lamps do: healthy ones never move.
		 */
		breathe?: boolean;
		/** Draws an offline lamp grey instead of red, for things that are often off, such as phones. */
		quiet?: boolean;
		/** Lights the lamp of an offline state instead of a ring, for an alert. */
		filled?: boolean;
		/** 7 pixels instead of 8, for the cards on the network map. */
		small?: boolean;
		class?: string;
	}

	let {
		status,
		breathe = false,
		quiet = false,
		filled = false,
		small = false,
		class: className
	}: Props = $props();

	const ring = $derived(!filled && (status === 'offline' || status === 'unknown'));
	const color = $derived(
		status === 'offline' && quiet ? 'border-fg-faint' : statusMeta[status].ring
	);
</script>

<span
	class={[
		'inline-block shrink-0 rounded-full',
		small ? 'size-1.75' : 'size-2',
		ring ? ['border-[1.5px]', color] : statusMeta[status].dot,
		breathe && (status === 'attention' || status === 'offline') && !quiet && 'animate-breathe',
		className
	]}
	aria-hidden="true"
></span>
