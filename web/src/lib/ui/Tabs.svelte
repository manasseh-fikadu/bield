<!--
	Tabs along a line, with a <TabPanel> for each inside. Arrow keys move
	between tabs.
-->
<script lang="ts">
	import type { Snippet } from 'svelte';
	import { Tabs } from 'bits-ui';

	interface Tab {
		value: string;
		label: string;
		/** A number after the label, such as how many items the tab holds. */
		count?: number;
	}

	interface Props {
		value?: string;
		tabs: Tab[];
		/** The <TabPanel>s. */
		children: Snippet;
		/** Something at the end of the row, such as a filter. */
		trailing?: Snippet;
		/** Classes for the row of tabs, such as how far it's indented. */
		listClass?: string;
		class?: string;
	}

	let {
		value = $bindable(),
		tabs,
		children,
		trailing,
		listClass,
		class: className
	}: Props = $props();
</script>

<Tabs.Root bind:value class={className}>
	<div class={['flex h-10 items-center gap-5 border-b border-line', listClass]}>
		<Tabs.List
			class="flex h-full min-w-0 [scrollbar-width:none] items-center gap-5 overflow-x-auto"
		>
			{#each tabs as tab (tab.value)}
				<Tabs.Trigger
					value={tab.value}
					class="flex h-full shrink-0 items-center gap-1.5 text-[13px] leading-5 text-fg-muted transition-colors hover:text-fg data-[state=active]:font-medium data-[state=active]:text-fg"
				>
					{tab.label}
					{#if tab.count !== undefined}
						<span class="font-mono text-[11px] font-normal text-fg-faint">{tab.count}</span>
					{/if}
				</Tabs.Trigger>
			{/each}
		</Tabs.List>
		{#if trailing}
			<div class="ml-auto flex shrink-0 items-center gap-3">{@render trailing()}</div>
		{/if}
	</div>
	{@render children()}
</Tabs.Root>
