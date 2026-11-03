<!-- Bield's mark: a roof, and a lamp lit under it. -->
<script lang="ts">
	interface Props {
		class?: string;
	}

	let { class: className = 'size-5' }: Props = $props();
</script>

<svg viewBox="0 0 20 20" class={className} fill="none" aria-hidden="true">
	<path
		d="M2.5 10.5 10 3l7.5 7.5"
		stroke="currentColor"
		stroke-width="2"
		stroke-linecap="round"
		stroke-linejoin="round"
	/>
	<circle cx="10" cy="13.5" r="3" fill="var(--color-amber)" />
</svg>
