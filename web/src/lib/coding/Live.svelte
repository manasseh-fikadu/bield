<!--
	A lit lamp and a word, for a session that is going now. The word is there so
	the lamp is never the only thing that says it.
-->
<script lang="ts">
	import StatusDot from '#lib/ui/StatusDot.svelte';

	interface Props {
		/** What it says, "Live" or such as "2 live". */
		label?: string;
		class?: string;
	}

	let { label = 'Live', class: className }: Props = $props();
</script>

<span class={['inline-flex items-center gap-1.5 whitespace-nowrap text-fg', className]}>
	<StatusDot status="online" />
	{label}
</span>
