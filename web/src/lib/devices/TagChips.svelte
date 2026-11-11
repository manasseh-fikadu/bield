<!--
	A device's Tailscale tags as small chips, without their "tag:" start, which
	wrap onto more lines when they don't fit.
-->
<script lang="ts">
	import { tagName } from '#lib/device.js';
	import Badge from '#lib/ui/Badge.svelte';

	interface Props {
		tags: string[];
		class?: string;
	}

	let { tags, class: className }: Props = $props();
</script>

{#if tags.length}
	<span class={['flex flex-wrap gap-1', className]}>
		{#each tags as tag (tag)}
			<Badge mono>{tagName(tag)}</Badge>
		{/each}
	</span>
{/if}
