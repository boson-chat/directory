<script lang="ts">
	import type { NetworkInput, OwnedNetwork } from '$lib/types';
	import { LIMITS, normaliseHostname, validateNetwork, type FormErrors } from '$lib/validate';
	import { LANGUAGES, langName } from '$lib/links';
	import type { DropdownOption } from '$lib/dropdown';
	import Dropdown from './Dropdown.svelte';
	import Toggle from './Toggle.svelte';
	import TagInput from './TagInput.svelte';

	/**
	 * The listing form. `create` asks for the address too; on edit the
	 * address is fixed (changing it would invalidate the DNS proof).
	 */
	let {
		mode,
		initial,
		submitLabel,
		onsubmit,
		oncancel
	}: {
		mode: 'create' | 'edit';
		initial?: Partial<OwnedNetwork>;
		submitLabel: string;
		/** Returns field errors from the server, if any. */
		onsubmit: (v: NetworkInput) => Promise<FormErrors | void>;
		oncancel?: () => void;
	} = $props();

	const uid = $props.id();
	// The form keeps its own copy; `initial` only seeds it.
	const seed = (() => initial ?? {})();
	let hostname = $state(seed.hostname ?? '');
	let port = $state(String(seed.port ?? 6697));
	let tls = $state(seed.tls ?? true);
	let name = $state(seed.name ?? '');
	let description = $state(seed.description ?? '');
	let tags = $state<string[]>([...(seed.tags ?? [])]);
	let languages = $state<string[]>([...(seed.languages ?? [])]);
	let nsfw = $state(seed.is_nsfw ?? false);
	let website = $state(seed.website_url ?? '');

	let errors = $state<FormErrors>({});
	let touched = $state(false);
	let busy = $state(false);

	const langOptions: DropdownOption[] = LANGUAGES.map((l) => ({
		value: l,
		label: langName(l),
		description: l
	}));

	let normalised = $derived.by(() => {
		const h = normaliseHostname(hostname);
		return 'host' in h && h.host !== hostname.trim() ? h.host : '';
	});

	function current() {
		return { hostname, port, name, description, tags, languages, website_url: website };
	}
	$effect(() => {
		if (touched) errors = validateNetwork(current(), mode === 'create');
	});

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		touched = true;
		errors = validateNetwork(current(), mode === 'create');
		if (Object.keys(errors).length) {
			document.querySelector<HTMLElement>(`#${uid}-form [aria-invalid="true"]`)?.focus();
			return;
		}
		const h = normaliseHostname(hostname);
		busy = true;
		try {
			const server = await onsubmit({
				hostname: 'host' in h ? h.host : hostname,
				port: Number(port),
				tls,
				name: name.trim(),
				description: description.trim(),
				tags,
				languages,
				is_nsfw: nsfw,
				website_url: website.trim()
			});
			if (server) errors = server;
		} finally {
			busy = false;
		}
	}
</script>

<form class="nform" id="{uid}-form" onsubmit={submit} novalidate>
	{#if mode === 'create'}
		<fieldset>
			<legend>Where people connect</legend>
			<div class="addr">
				<label class="field host">
					<span>Hostname</span>
					<input
						type="text"
						bind:value={hostname}
						placeholder="irc.example.org"
						autocomplete="off"
						autocapitalize="none"
						spellcheck="false"
						inputmode="url"
						aria-invalid={!!errors.hostname || undefined}
						aria-describedby="{uid}-host"
					/>
					<small id="{uid}-host" class:err={errors.hostname}>
						{errors.hostname ??
							(normalised
								? `Saved as ${normalised}.`
								: 'The name you’ll prove with a DNS record. It can’t be changed later.')}
					</small>
				</label>
				<label class="field port">
					<span>Port</span>
					<input
						type="text"
						inputmode="numeric"
						bind:value={port}
						aria-invalid={!!errors.port || undefined}
						aria-describedby="{uid}-port"
					/>
					{#if errors.port}<small id="{uid}-port" class="err">{errors.port}</small>{/if}
				</label>
			</div>
			<Toggle
				label="TLS"
				detail="Clients connect with encryption. Leave it on unless the port only speaks plain text."
				bind:checked={tls}
				onchange={(v) => {
					if (v && port === '6667') port = '6697';
					if (!v && port === '6697') port = '6667';
				}}
			/>
		</fieldset>
	{/if}

	<fieldset>
		<legend class:sr-only={mode === 'edit'}>How it’s listed</legend>
		<label class="field">
			<span>Network name</span>
			<input
				type="text"
				bind:value={name}
				maxlength={LIMITS.name + 10}
				aria-invalid={!!errors.name || undefined}
				aria-describedby="{uid}-name"
			/>
			{#if errors.name}<small id="{uid}-name" class="err">{errors.name}</small>{/if}
		</label>
		<label class="field">
			<span>Description</span>
			<textarea
				bind:value={description}
				rows="4"
				aria-invalid={!!errors.description || undefined}
				aria-describedby="{uid}-desc"
				placeholder="Who it’s for and what people talk about."></textarea>
			<small id="{uid}-desc" class:err={errors.description}
				>{errors.description ?? `${description.length} / ${LIMITS.description}`}</small
			>
		</label>
		<div class="row2">
			<div class="field">
				<span id="{uid}-tags-l">Topics</span>
				<TagInput bind:tags id="{uid}-tags" describedby="{uid}-tags-h" invalid={!!errors.tags} />
				<small id="{uid}-tags-h" class:err={errors.tags}
					>{errors.tags ?? `Up to ${LIMITS.tags}. Press Enter after each one.`}</small
				>
			</div>
			<div class="field">
				<span id="{uid}-lang-l">Languages</span>
				<Dropdown
					options={langOptions}
					multiple
					bind:values={languages}
					labelledby="{uid}-lang-l"
					describedby="{uid}-lang-h"
					placeholder="Choose languages"
					summary={(c) => (c.length ? c.map((o) => o.label).join(', ') : 'Choose languages')}
				/>
				<small id="{uid}-lang-h" class:err={errors.languages}
					>{errors.languages ?? 'The languages people chat in.'}</small
				>
			</div>
		</div>
		<label class="field">
			<span>Website</span>
			<input
				type="url"
				bind:value={website}
				placeholder="https://"
				spellcheck="false"
				autocapitalize="none"
				aria-invalid={!!errors.website_url || undefined}
				aria-describedby="{uid}-web"
			/>
			{#if errors.website_url}<small id="{uid}-web" class="err">{errors.website_url}</small>{/if}
		</label>
		<Toggle
			label="Adult content (18+)"
			detail="Hidden from the directory unless someone turns on 18+ listings."
			bind:checked={nsfw}
		/>
	</fieldset>

	<div class="actions">
		{#if oncancel}<button class="btn btn-ghost" type="button" onclick={oncancel}>Cancel</button>{/if}
		<button class="btn btn-primary" type="submit" disabled={busy}>{busy ? 'Saving…' : submitLabel}</button>
	</div>
</form>

<style>
	.nform {
		display: grid;
		gap: var(--space-8);
		max-width: 720px;
	}
	fieldset {
		margin: 0;
		padding: 0;
		border: 0;
		display: grid;
		gap: var(--space-5);
		min-width: 0;
	}
	legend {
		padding: 0;
		margin-bottom: var(--space-4);
		font-family: var(--font-display);
		font-weight: 600;
		font-size: var(--text-base);
		color: var(--fg);
	}
	.addr {
		display: grid;
		grid-template-columns: minmax(0, 1fr) 120px;
		gap: var(--space-4);
	}
	.row2 {
		display: grid;
		grid-template-columns: repeat(2, minmax(0, 1fr));
		gap: var(--space-4);
	}
	textarea {
		resize: vertical;
		font-family: var(--font-body);
		line-height: 1.5;
	}
	[aria-invalid='true'] {
		border-color: var(--danger);
	}
	small.err {
		color: var(--danger-text);
	}
	.actions {
		display: flex;
		justify-content: flex-end;
		gap: var(--space-2);
		padding-top: var(--space-4);
		border-top: 1px solid var(--border-soft);
	}
	@media (max-width: 560px) {
		.addr,
		.row2 {
			grid-template-columns: minmax(0, 1fr);
		}
		.actions .btn {
			flex: 1;
		}
	}
</style>
