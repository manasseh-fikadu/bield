<!--
	Where admins make an API token: a name, a role and how long it works. Once
	the hub has made it, the dialog shows its secret, the only time anyone
	sees it, with how to use it.
-->
<script lang="ts">
	import { untrack } from 'svelte';
	import TriangleAlert from '@lucide/svelte/icons/triangle-alert';
	import { makeToken } from '#lib/api/client.js';
	import {
		DefaultTokenDays,
		RoleAdmin,
		RoleViewer,
		type APIToken,
		type NewAPITokenResult,
		type Role
	} from '#lib/api/types.js';
	import { roleLabel } from '#lib/access.js';
	import { shortDate } from '#lib/format.js';
	import { maxTokenNameLength, sentence, tokenDayOptions, tokenNameProblem } from '#lib/tokens.js';
	import Button from '#lib/ui/Button.svelte';
	import CodeLine from '#lib/ui/CodeLine.svelte';
	import CopyButton from '#lib/ui/CopyButton.svelte';
	import Dialog from '#lib/ui/Dialog.svelte';
	import Field from '#lib/ui/Field.svelte';
	import Input from '#lib/ui/Input.svelte';
	import Select from '#lib/ui/Select.svelte';

	interface Props {
		open?: boolean;
		/** The names of the tokens there are, which a new one can't take. */
		taken: readonly string[];
		/** The address the CLI reaches Bield at, for the commands shown. */
		hubUrl: string;
		/** Called with the token the hub made. */
		onmade: (token: APIToken) => void;
	}

	let { open = $bindable(false), taken, hubUrl, onmade }: Props = $props();

	const id = $props.id();

	const roles: Role[] = [RoleViewer, RoleAdmin];

	let name = $state('');
	let role = $state<Role>(RoleViewer);
	let days = $state(DefaultTokenDays);
	let saving = $state(false);
	/** Whether anything was typed, so the form only complains once it's started. */
	let touched = $state(false);
	/** Why the hub refused the last try. */
	let refusal = $state('');
	/** The token the hub made, with its secret, once it has. */
	let made = $state.raw<NewAPITokenResult | null>(null);

	// The form starts empty each time the dialog opens, and stays put while
	// it's open, even as the list behind it refreshes.
	$effect.pre(() => {
		if (open) {
			untrack(() => {
				name = '';
				role = RoleViewer;
				days = DefaultTokenDays;
				touched = false;
				refusal = '';
				made = null;
			});
		}
	});

	const problem = $derived(tokenNameProblem(name, taken));
	const expires = $derived(shortDate(Date.now() + days * 86_400_000));

	function daysLabel(n: number): string {
		return n === 365 ? '1 year' : `${n} days`;
	}

	async function save(e: SubmitEvent) {
		e.preventDefault();
		if (made) {
			return;
		}
		if (problem) {
			touched = true;
			return;
		}
		saving = true;
		refusal = '';
		try {
			const result = await makeToken({ name: name.trim(), role, days });
			made = result;
			onmade(result.token);
		} catch (err) {
			refusal = sentence(err instanceof Error ? err.message : String(err));
		} finally {
			saving = false;
		}
	}

	const connectCommand = $derived(`bield connect ${hubUrl} --with-token`);
	const envCommand = $derived(`BIELD_URL=${hubUrl} BIELD_TOKEN=<token> bield devices`);
</script>

<Dialog
	bind:open
	title={made ? `Copy the token ${made.token.name}` : 'Make an API token'}
	description={made
		? "Bield keeps only a hash of it, so this is the only time it's shown. If you lose it, revoke it and make another."
		: 'For a script, a CI job or an AI agent on a tagged machine, which Tailscale has no person for.'}
>
	{#if made}
		<div class="space-y-4">
			<div
				class="flex items-center gap-2 rounded-lg border border-line-strong bg-canvas py-1.5 pr-1.5 pl-3"
			>
				<code
					class="min-w-0 flex-1 overflow-x-auto font-mono text-[12.5px] whitespace-nowrap text-fg select-all"
					aria-label="The token">{made.secret}</code
				>
				<CopyButton value={made.secret} label="Copy the token" />
			</div>
			<div class="space-y-2">
				<p class="text-[13px] text-fg-muted">
					To save it on a machine, run this there. It reads the token from standard input, so paste
					it or pipe it in.
				</p>
				<CodeLine command={connectCommand} />
			</div>
			<div class="space-y-2">
				<p class="text-[13px] text-fg-muted">
					Or give it to a script or CI job in its environment, such as from a secret:
				</p>
				<CodeLine command={envCommand} />
				<p class="text-[12px] text-fg-subtle">
					Programs that call the API send it as
					<code class="font-mono text-[11.5px] text-fg-muted"
						>Authorization: Bearer &lt;token&gt;</code
					>.
				</p>
			</div>
		</div>
	{:else}
		<form
			id="{id}-form"
			class="grid gap-4 sm:grid-cols-2"
			oninput={() => {
				touched = true;
				refusal = '';
			}}
			onsubmit={save}
			novalidate
		>
			<div class="sm:col-span-2">
				<Field
					label="Name"
					hint="Letters, digits, dots, dashes and underscores. It's shown here and in the activity log."
				>
					<Input
						bind:value={name}
						placeholder="deploy"
						maxlength={maxTokenNameLength}
						autocomplete="off"
						autocapitalize="off"
						spellcheck={false}
					/>
				</Field>
			</div>
			<Field label="Role" hint="The most it can do. It never does more than you can.">
				<Select bind:value={role}>
					{#each roles as r (r)}
						<option value={r}>{roleLabel(r)}</option>
					{/each}
				</Select>
			</Field>
			<Field label="Expires" hint="On {expires}.">
				<Select bind:value={() => String(days), (v) => (days = Number(v))}>
					{#each tokenDayOptions as n (n)}
						<option value={String(n)}>{daysLabel(n)}</option>
					{/each}
				</Select>
			</Field>
			{#if role === RoleAdmin}
				<p
					class="flex gap-2 rounded-lg border border-line bg-canvas px-3 py-2 text-[12.5px] text-amber-fg sm:col-span-2"
				>
					<TriangleAlert class="mt-0.5 size-3.5 shrink-0" />
					An admin token can change settings, restart containers and edit files on your machines. Give
					it only to what needs that.
				</p>
			{/if}
		</form>
	{/if}
	{#snippet footer()}
		{#if made}
			<Button variant="primary" onclick={() => (open = false)}>Done</Button>
		{:else}
			<p class="mr-auto self-center text-[12.5px] text-red-fg" aria-live="polite">
				{refusal || (touched ? problem : '')}
			</p>
			<Button variant="ghost" onclick={() => (open = false)}>Cancel</Button>
			<Button
				variant="primary"
				type="submit"
				form="{id}-form"
				loading={saving}
				disabled={!!problem}
			>
				Make token
			</Button>
		{/if}
	{/snippet}
</Dialog>
