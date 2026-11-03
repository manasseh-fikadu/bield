<!--
	A row of buttons that picks one of a few options, such as a time range.
	Arrow keys move between them.
-->
<script lang="ts" generics="T extends string">
	import type { Snippet } from 'svelte';
	import { RadioGroup } from 'bits-ui';

	interface Option {
		value: T;
		/** The option's text, and its name for screen readers if `option` draws it. */
		label: string;
	}

	interface Props {
		value: T;
		options: Option[];
		/** What's being picked, for screen readers, such as "Time range". */
		label: string;
		/** Draws an option in place of its label. */
		option?: Snippet<[Option]>;
	}

	let { value = $bindable(), options, label, option }: Props = $props();
</script>

<RadioGroup.Root
	bind:value={() => value, (v) => (value = v as T)}
	orientation="horizontal"
	aria-label={label}
	class="inline-flex h-7.5 items-center rounded-md border border-line-strong p-0.5 pointer-coarse:h-10"
>
	{#each options as o (o.value)}
		<RadioGroup.Item
			value={o.value}
			aria-label={option ? o.label : undefined}
			class="flex h-full items-center rounded-[4px] px-2.25 text-[12px] leading-4 text-fg-muted transition-colors hover:text-fg data-[state=checked]:bg-hover data-[state=checked]:font-medium data-[state=checked]:text-fg"
		>
			{#if option}
				{@render option(o)}
			{:else}
				{o.label}
			{/if}
		</RadioGroup.Item>
	{/each}
</RadioGroup.Root>
