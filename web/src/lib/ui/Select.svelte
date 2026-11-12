<!--
	The browser's own drop-down list, in Bield's style. Give it a label, such as
	with <Field>, or aria-label. size="sm" is as tall as <Segmented>, for a
	card's header.
-->
<script lang="ts">
	import ChevronDown from '@lucide/svelte/icons/chevron-down';
	import type { HTMLSelectAttributes } from 'svelte/elements';
	import { fieldClass } from './button.js';

	interface Props extends Omit<HTMLSelectAttributes, 'size'> {
		size?: 'md' | 'sm';
	}

	let { value = $bindable(), size = 'md', class: className, children, ...rest }: Props = $props();

	// fieldClass with a smaller text and padding, which can't be put over its own.
	const smallClass =
		'h-7.5 rounded-md border border-line-strong bg-transparent pr-7 pl-2.5 text-[12px] text-fg-muted transition-colors hover:border-line-heavy hover:text-fg focus:border-fg-faint focus:outline-none disabled:opacity-50 pointer-coarse:h-10';
</script>

<span class={size === 'sm' ? 'relative inline-block' : 'relative block'}>
	<select
		bind:value
		class={[size === 'sm' ? smallClass : [fieldClass, 'h-9 pr-8'], 'appearance-none', className]}
		{...rest}
	>
		{@render children?.()}
	</select>
	<ChevronDown
		class={[
			'pointer-events-none absolute top-1/2 -translate-y-1/2 text-fg-subtle',
			size === 'sm' ? 'right-2 size-3.5' : 'right-2.5 size-4'
		]}
	/>
</span>
