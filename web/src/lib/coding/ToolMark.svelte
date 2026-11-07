<!--
	A tool's color as a small square. Lamps are round and mean a status, so a
	tool's color never looks like one, whatever the color is.
-->
<script lang="ts">
	import { toolColor } from './coding.js';

	interface Props {
		tool: string;
		class?: string;
	}

	let { tool, class: className }: Props = $props();
</script>

<span
	class={['inline-block size-2 shrink-0 rounded-[2px]', className]}
	style:background={toolColor(tool)}
	aria-hidden="true"
></span>
