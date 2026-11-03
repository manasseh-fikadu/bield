<!-- Whether the stream from the hub is live, and when the latest data came. -->
<script lang="ts">
	import { timeOfDay } from '#lib/format.js';
	import { live } from '#lib/live.svelte.js';

	const labels = { live: 'Live', connecting: 'Connecting', reconnecting: 'Reconnecting' };
</script>

<!-- On a phone only the lamp shows, with the state as its tooltip. -->
<div
	class="flex items-center gap-2 text-[12px] leading-4 text-fg-faint"
	title={labels[live.connection]}
>
	<span
		class={[
			'size-1.5 shrink-0 rounded-[3px]',
			live.connection === 'live'
				? 'bg-lamp'
				: live.connection === 'reconnecting'
					? 'bg-amber'
					: 'border border-fg-faint'
		]}
		aria-hidden="true"
	></span>
	<!-- Only the state is announced: the time next to it changes with every snapshot. -->
	<span class="hidden md:inline">
		<span class={[live.connection === 'reconnecting' && 'text-amber']} role="status"
			>{labels[live.connection]}</span
		>{#if live.receivedAt > 0}, updated {timeOfDay(live.receivedAt)}{/if}
	</span>
</div>
