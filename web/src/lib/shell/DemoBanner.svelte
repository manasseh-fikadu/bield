<!--
	What every page of a demo says under the header: the machines are made up,
	and what needs a real one is turned off.
-->
<script lang="ts">
	import FlaskConical from '@lucide/svelte/icons/flask-conical';
</script>

<p
	role="note"
	class="flex flex-wrap items-center gap-x-2 gap-y-0.5 border-b border-line bg-raised px-4 py-2 text-[12px] leading-4 text-fg-muted sm:px-6 lg:border-x"
>
	<FlaskConical class="size-3.5 shrink-0" aria-hidden="true" />
	<span>
		This is a demo with made-up machines. Terminals, files and changes to Docker are turned off.
	</span>
	<a
		href="https://github.com/manasseh-fikadu/bield"
		class="text-fg underline decoration-line-heavy underline-offset-2 hover:decoration-fg"
		target="_blank"
		rel="noreferrer">Run Bield on your own tailnet</a
	>
</p>
