<!-- A bordered panel, with an optional header of title, description and actions. -->
<script lang="ts">
	import type { Snippet } from 'svelte';

	interface Props {
		/** Lets a link on the page jump to the card. */
		id?: string;
		title?: string;
		description?: string;
		/** Buttons or links at the right of the header. */
		actions?: Snippet;
		children: Snippet;
		class?: string;
		/** Classes for the part under the header. */
		bodyClass?: string;
	}

	let { id, title, description, actions, children, class: className, bodyClass }: Props = $props();
</script>

<section {id} class={['overflow-hidden card', className]}>
	{#if title || actions}
		<header class="flex items-start justify-between gap-3 border-b border-line px-4 py-3">
			<div class="min-w-0">
				{#if title}
					<h2 class="text-[14px] leading-5 font-medium text-fg">{title}</h2>
				{/if}
				{#if description}
					<p class="mt-0.5 text-[13px] leading-5 text-fg-subtle">{description}</p>
				{/if}
			</div>
			{#if actions}
				<div class="flex shrink-0 items-center gap-1.5">{@render actions()}</div>
			{/if}
		</header>
	{/if}
	<div class={bodyClass}>{@render children()}</div>
</section>
