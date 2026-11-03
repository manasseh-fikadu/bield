<!--
	What can be done about an alert: admins can acknowledge and snooze it, and
	anyone can read its notes. A resolved alert has only its notes.
-->
<script lang="ts">
	import AdminOnly from '#lib/AdminOnly.svelte';
	import { snoozeOptions } from '#lib/alerts.js';
	import { ackAlert, snoozeAlert } from '#lib/api/client.js';
	import type { Alert } from '#lib/api/types.js';
	import Button from '#lib/ui/Button.svelte';
	import Menu from '#lib/ui/Menu.svelte';
	import MenuItem from '#lib/ui/MenuItem.svelte';
	import MenuSeparator from '#lib/ui/MenuSeparator.svelte';
	import NotesDialog from './NotesDialog.svelte';

	interface Props {
		alert: Alert;
		/** Makes acknowledging the button that stands out. */
		primary?: boolean;
		/** Draws everything as plain text, for a line across a page. */
		plain?: boolean;
		class?: string;
	}

	let { alert, primary = false, plain = false, class: className }: Props = $props();

	let busy = $state(false);
	let notesOpen = $state(false);

	const resolved = $derived(alert.state === 'resolved');

	async function act(action: () => Promise<unknown>) {
		busy = true;
		try {
			await action();
		} catch {
			// The client showed the error.
		} finally {
			busy = false;
		}
	}
</script>

{#snippet text(label: string, onclick?: () => void, props: Record<string, unknown> = {})}
	<button
		{...props}
		type="button"
		disabled={busy}
		class="flex h-6.5 items-center rounded-[5px] px-1 text-[12px] leading-4 text-fg-muted transition-colors hover:text-fg disabled:opacity-50 pointer-coarse:h-8"
		{onclick}
	>
		{label}
	</button>
{/snippet}

<div class={['flex flex-wrap items-center gap-1.5', className]}>
	{#if !resolved}
		<AdminOnly>
			{#if alert.ackedAt}
				{#if plain}
					{@render text('Unacknowledge', () => act(() => ackAlert(alert.id, false)))}
				{:else}
					<Button size="xs" disabled={busy} onclick={() => act(() => ackAlert(alert.id, false))}>
						Unacknowledge
					</Button>
				{/if}
			{:else if plain}
				{@render text('Acknowledge', () => act(() => ackAlert(alert.id, true)))}
			{:else}
				<Button
					size="xs"
					variant={primary ? 'primary' : 'secondary'}
					disabled={busy}
					onclick={() => act(() => ackAlert(alert.id, true))}
				>
					Acknowledge
				</Button>
			{/if}
			<Menu align={plain ? 'end' : 'start'}>
				{#snippet trigger({ props })}
					{#if plain}
						{@render text(alert.state === 'snoozed' ? 'Snoozed' : 'Snooze', undefined, props)}
					{:else}
						<Button {...props} size="xs" disabled={busy}>
							{alert.state === 'snoozed' ? 'Snoozed' : 'Snooze'}
						</Button>
					{/if}
				{/snippet}
				{#each snoozeOptions as o (o.minutes)}
					<MenuItem onSelect={() => act(() => snoozeAlert(alert.id, o.minutes))}>
						For {o.label}
					</MenuItem>
				{/each}
				{#if alert.state === 'snoozed'}
					<MenuSeparator />
					<MenuItem onSelect={() => act(() => snoozeAlert(alert.id, null))}>Wake it</MenuItem>
				{/if}
			</Menu>
		</AdminOnly>
	{/if}
	{@render text(
		`Notes${alert.notes.length ? ` (${alert.notes.length})` : ''}`,
		() => (notesOpen = true)
	)}
</div>
<NotesDialog {alert} bind:open={notesOpen} />
