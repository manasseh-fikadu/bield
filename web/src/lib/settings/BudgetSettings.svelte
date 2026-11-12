<!--
	The AI usage budget: what the AI tools on the machines can use over a
	window before the AI usage budget rule raises an alert, a warning at 80%
	and a critical alert once it's spent. A budget of 0 is none.
-->
<script lang="ts">
	import { changeSettings } from '#lib/api/client.js';
	import type { SettingsView } from '#lib/api/types.js';
	import { browserTimezone, timezoneNames } from '#lib/notifications.js';
	import { budgetProblem, budgetWindows, changedFields } from '#lib/settings.js';
	import Button from '#lib/ui/Button.svelte';
	import Field from '#lib/ui/Field.svelte';
	import Input from '#lib/ui/Input.svelte';
	import Select from '#lib/ui/Select.svelte';
	import { Draft } from './draft.svelte.js';
	import FormCard from './FormCard.svelte';
	import NumberInput from './NumberInput.svelte';

	interface Props {
		view: SettingsView;
		onsaved: (view: SettingsView) => void;
	}

	let { view, onsaved }: Props = $props();

	const id = $props.id();
	const draft = new Draft(() => view.settings.aiBudget);
	let saving = $state(false);

	const problem = $derived(budgetProblem(draft.value));
	const ruleOff = $derived(!view.settings.rules.aiBudget);
	const daily = $derived(draft.value.window !== '24h');
	const zones = timezoneNames();
	const mine = browserTimezone();

	/** Tokens are typed in millions, since a budget of them is in the millions or more. */
	const million = 1_000_000;

	async function save() {
		saving = true;
		try {
			const edited = $state.snapshot(draft.value);
			// The days of Bield's machine are rarely the admin's.
			if (edited.timezone === '' && mine !== '' && daily) {
				edited.timezone = mine;
			}
			const saved = await changeSettings({ aiBudget: changedFields(edited, draft.base) });
			draft.adopt(saved.settings.aiBudget);
			onsaved(saved);
		} catch {
			// changeSettings showed the error.
		} finally {
			saving = false;
		}
	}
</script>

{#snippet amounts(who: string, cost: 'cost' | 'machineCost', tokens: 'tokens' | 'machineTokens')}
	<div class="space-y-2">
		<p class="text-[13px] font-medium text-fg">{who}</p>
		<div class="flex flex-wrap items-center gap-x-4 gap-y-2">
			<NumberInput
				bind:value={draft.value[cost]}
				unit="dollars"
				min={0}
				step={0.01}
				placeholder="0"
				label="{who}: budget in dollars"
			/>
			<NumberInput
				bind:value={
					() => draft.value[tokens] / million,
					(v) => (draft.value[tokens] = Math.round(v * million))
				}
				unit="million tokens"
				min={0}
				step={0.1}
				placeholder="0"
				label="{who}: budget in millions of tokens"
			/>
		</div>
	</div>
{/snippet}

<FormCard
	title="AI usage budget"
	description="What the AI tools on your machines can use before an alert: a warning at 80% of a budget, and a critical alert once it's spent. Dollars are the API estimate the AI usage page shows, and tokens are the tokens processed. 0 is no budget."
	dirty={draft.dirty}
	{saving}
	{problem}
	onsave={save}
	onreset={() => draft.reset()}
>
	<div class="max-w-xl space-y-5">
		<Field
			label="Over"
			hint={daily
				? 'Today and the days before it, as the AI usage page counts them.'
				: 'The last 24 hours.'}
		>
			<div class="max-w-48">
				<Select bind:value={draft.value.window}>
					{#each budgetWindows as w (w.value)}
						<option value={w.value}>{w.label}</option>
					{/each}
				</Select>
			</div>
		</Field>
		{#if daily}
			<Field
				label="Time zone"
				hint="Days start at midnight in this zone. Empty is the time zone of the machine Bield runs on."
			>
				<span class="flex gap-2">
					<Input
						id="{id}-zone"
						bind:value={draft.value.timezone}
						list="{id}-zones"
						placeholder="Europe/Berlin"
						autocomplete="off"
						autocapitalize="off"
						spellcheck={false}
					/>
					{#if mine !== '' && draft.value.timezone !== mine}
						<Button onclick={() => (draft.value.timezone = mine)}>Use mine</Button>
					{/if}
				</span>
				<datalist id="{id}-zones">
					{#each zones as z (z)}
						<option value={z}></option>
					{/each}
				</datalist>
			</Field>
		{/if}
		<div class="grid gap-5 border-t border-line pt-5 sm:grid-cols-2">
			{@render amounts('Every machine together', 'cost', 'tokens')}
			{@render amounts('Each machine', 'machineCost', 'machineTokens')}
		</div>
	</div>
	{#snippet note()}
		{#if ruleOff}
			The AI usage budget rule is off above, so these raise no alerts.
		{:else}
			Bield adds up what the AI tools used once a minute.
		{/if}
	{/snippet}
</FormCard>
