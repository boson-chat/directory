<script lang="ts">
	import { onDestroy } from 'svelte';
	import { directory, ApiError } from '$lib/api';
	import type { Listing, MoveVerifyResponse, VerifyReport } from '$lib/types';
	import { address, digCommand } from '$lib/links';
	import { normaliseHostname, portError } from '$lib/validate';
	import { datetime } from '$lib/format';
	import { toasts, errorText } from '$lib/toasts.svelte';
	import CopyField from './CopyField.svelte';
	import Confirm from './Confirm.svelte';
	import Notice from './Notice.svelte';
	import Toggle from './Toggle.svelte';
	import ResolverReport from './ResolverReport.svelte';
	import Time from './Time.svelte';

	/**
	 * Moves a verified listing to a new hostname. The move waits for the new
	 * hostname's own TXT record; until then the listing keeps its address.
	 */
	let { listing = $bindable() }: { listing: Listing } = $props();
	const uid = $props.id();
	let n = $derived(listing.network);
	let move = $derived(listing.move ?? null);

	let hostname = $state('');
	let port = $state(String(listing.network.port));
	let tls = $state(listing.network.tls);
	let errors = $state<{ hostname?: string; port?: string }>({});
	let busy = $state(false);

	let checking = $state(false);
	let report = $state<VerifyReport | null>(null);
	let problem = $state<{ kind: 'warn' | 'bad'; text: string } | null>(null);
	let wait = $state(0);
	let timer: ReturnType<typeof setInterval> | undefined;
	onDestroy(() => clearInterval(timer));

	let normalised = $derived.by(() => {
		const h = normaliseHostname(hostname);
		return 'host' in h && h.host !== hostname.trim() ? h.host : '';
	});
	let sub = $derived(move ? move.hostname.split('.').slice(0, -2).join('.') : '');

	function cooldown(s: number) {
		wait = s;
		clearInterval(timer);
		timer = setInterval(() => {
			wait = Math.max(0, wait - 1);
			if (!wait) clearInterval(timer);
		}, 1000);
	}

	async function start(e?: SubmitEvent) {
		e?.preventDefault();
		const h = normaliseHostname(hostname);
		errors = {};
		if ('error' in h) errors.hostname = h.error;
		const pe = portError(port);
		if (pe) errors.port = pe;
		if (errors.hostname || errors.port || !('host' in h)) return;
		busy = true;
		try {
			const sameHost = h.host === n.hostname;
			listing = await directory.requestMove(n.id, { hostname: h.host, port: Number(port), tls });
			report = null;
			problem = null;
			if (sameHost) toasts.push(`${n.name} now listens on ${address(listing.network)}.`);
			else toasts.push('Move started. Add the TXT record at the new hostname, then check it.');
			hostname = '';
		} catch (err) {
			if (err instanceof ApiError && ['invalid_hostname', 'hostname_taken', 'blocked'].includes(err.code)) {
				errors.hostname = err.message;
			} else if (err instanceof ApiError && err.code === 'invalid_request') {
				errors.hostname = err.message;
			} else {
				toasts.push(errorText(err), 'bad');
			}
		} finally {
			busy = false;
		}
	}

	async function check() {
		if (!move) return;
		checking = true;
		problem = null;
		try {
			const r = await directory.verifyMove(n.id);
			report = null;
			listing = { network: r.network, verification: r.verification, move: null };
			toasts.push(`${n.name} moved to ${r.network.hostname}.`);
		} catch (e) {
			if (
				e instanceof ApiError &&
				e.status === 409 &&
				e.data &&
				typeof e.data === 'object' &&
				'report' in e.data
			) {
				const r = e.data as MoveVerifyResponse;
				report = r.report;
				listing = { network: r.network, verification: r.verification, move: r.move };
			} else if (e instanceof ApiError && e.status === 410) {
				report = null;
				listing = { ...listing, move: move ? { ...move, expired: true } : null };
			} else if (e instanceof ApiError && e.status === 429) {
				cooldown(e.retryAfter || 30);
				problem = { kind: 'bad', text: 'DNS checks are limited. Give the record a moment to spread first.' };
			} else {
				problem = { kind: 'bad', text: errorText(e) };
			}
		} finally {
			checking = false;
		}
	}

	async function reissue() {
		if (!move) return;
		try {
			listing = await directory.requestMove(n.id, {
				hostname: move.hostname,
				port: move.port,
				tls: move.tls
			});
			report = null;
			problem = null;
			toasts.push('New record issued. Replace the old TXT value with it.');
		} catch (e) {
			toasts.push(errorText(e), 'bad');
			throw e;
		}
	}

	async function cancel() {
		try {
			listing = await directory.cancelMove(n.id);
			report = null;
			problem = null;
			toasts.push(`Move cancelled. ${n.name} stays at ${n.hostname}.`);
		} catch (e) {
			toasts.push(errorText(e), 'bad');
			throw e;
		}
	}
</script>

{#if move}
	<ol class="steps">
		<li class="done">
			<h3>Move requested</h3>
			<div class="route" aria-label="From {address(move.from)} to {address(move.to)}">
				<span class="addr old">{address(move.from)}</span>
				<span class="arrow" aria-hidden="true">→</span>
				<span class="addr new">{address(move.to)}</span>
			</div>
			<p>
				Until the check passes, the listing stays at <code>{move.from.hostname}</code> and nothing changes for the
				people who use it.
			</p>
		</li>
		<li>
			<h3>Add this TXT record at {move.hostname}</h3>
			<div class="rec">
				<CopyField label="Name" value={move.record_name ?? ''} />
				<div class="type"><span class="tl">Type</span><span class="tv">TXT</span></div>
				<CopyField label="Value" value={move.record_value ?? ''} />
			</div>
			<p class="small">
				{#if sub}Some DNS providers want only <code>_boson.{sub}</code> as the name, without the domain.{/if}
				It’s a new record for the new hostname; keep the one at <code>_boson.{move.from.hostname}</code> until
				the move is done.
				{#if move.expires_at}
					{#if move.expired}<strong class="bad">This record expired {datetime(move.expires_at)}.</strong>
					{:else}Check it before {datetime(move.expires_at)}; after that you’ll need a new one.{/if}
				{/if}
			</p>
			<CopyField label="See what the world sees" value={digCommand(move.hostname)} />
		</li>
		<li>
			<h3>Check it</h3>
			<p>
				All three public resolvers must see the record. Then the listing switches to the new address and keeps
				its page, stats, channels and theme; directories that import it follow.
				{#if move.last_checked_at}Last checked <Time at={move.last_checked_at} />.{/if}
			</p>
			<div class="acts">
				<button
					class="btn btn-primary btn-small"
					onclick={check}
					disabled={checking || wait > 0 || move.expired}
					>{checking ? 'Checking…' : wait ? `Check again in ${wait}s` : 'Check DNS now'}</button
				>
				{#if move.expired}
					<Confirm title="Issue a new record?" action="Issue new record" onconfirm={reissue}>
						{#snippet trigger(open)}<button class="btn btn-small" onclick={open}>New record</button>{/snippet}
						The expired value stops working. Replace the TXT record at {move.hostname} with the new one before you
						check.
					</Confirm>
				{/if}
				<Confirm title="Cancel the move?" action="Cancel move" onconfirm={cancel}>
					{#snippet trigger(open)}<button class="btn btn-small btn-ghost" onclick={open}>Cancel move</button
						>{/snippet}
					{n.name} stays at <strong>{move.from.hostname}</strong>. You can start a new move later.
				</Confirm>
			</div>
			{#if move.expired && !problem}
				<Notice tone="warn"
					>This record expired before all three resolvers saw it. Issue a new one to try again.</Notice
				>
			{/if}
			{#if problem}<Notice tone={problem.kind}>{problem.text}</Notice>{/if}
			{#if report}
				<ResolverReport
					{report}
					hint="DNS changes can take a few minutes to reach every resolver. The directory also checks a pending move every hour on its own."
				/>
			{/if}
		</li>
	</ol>
{:else}
	<form class="start" onsubmit={start} novalidate>
		<div class="addr-fields">
			<label class="field host">
				<span>New hostname</span>
				<input
					type="text"
					bind:value={hostname}
					oninput={() => (errors = { ...errors, hostname: undefined })}
					placeholder="irc.boson.example.org"
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
							: 'You’ll prove it with its own TXT record, like the first time.')}
				</small>
			</label>
			<label class="field port">
				<span>Port</span>
				<input
					type="text"
					inputmode="numeric"
					bind:value={port}
					oninput={() => (errors = { ...errors, port: undefined })}
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
		<div class="acts">
			<button class="btn btn-primary btn-small" type="submit" disabled={busy}
				>{busy ? 'Starting…' : 'Start move'}</button
			>
			<span class="muted small"
				>The listing stays at <code>{address(n)}</code> until the new record checks out.</span
			>
		</div>
	</form>
{/if}

{#if n.previous_hostnames?.length}
	<p class="prev small">
		Previously listed as
		{#each n.previous_hostnames as h, i (h)}<code>{h}</code>{i < n.previous_hostnames.length - 1
				? ', '
				: ''}{/each}. Lookups by an old hostname and directories that import this listing follow it here.
	</p>
{/if}

<style>
	.start {
		display: grid;
		gap: var(--space-4);
		max-width: 640px;
	}
	.addr-fields {
		display: grid;
		grid-template-columns: minmax(0, 1fr) 120px;
		gap: var(--space-3);
	}
	.acts {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2) var(--space-3);
	}
	.small {
		font-size: var(--text-xs);
		line-height: 1.6;
	}
	.muted {
		color: var(--fg-2);
	}
	.prev {
		margin-top: var(--space-5);
		color: var(--fg-2);
		max-width: 72ch;
	}
	.route {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2) var(--space-3);
		font-family: var(--font-mono);
		font-size: var(--text-sm);
	}
	.route .addr {
		padding: 2px var(--space-2);
		border: 1px solid var(--border-soft);
		border-radius: var(--radius-sm);
		overflow-wrap: anywhere;
	}
	.route .old {
		color: var(--fg-2);
	}
	.route .new {
		color: var(--fg);
		border-color: var(--accent);
	}
	.arrow {
		color: var(--fg-quiet);
	}
	.bad {
		color: var(--danger-text);
		font-weight: 600;
	}

	/* Numbered steps, as in DnsSteps. */
	.steps {
		list-style: none;
		margin: 0;
		padding: 0;
		counter-reset: step;
		display: grid;
	}
	.steps > li {
		counter-increment: step;
		position: relative;
		padding: 0 0 var(--space-8) 44px;
		display: grid;
		gap: var(--space-3);
		min-width: 0;
	}
	.steps > li::before {
		content: counter(step);
		position: absolute;
		left: 0;
		top: -2px;
		width: 26px;
		height: 26px;
		border-radius: 50%;
		display: grid;
		place-items: center;
		font-family: var(--font-mono);
		font-size: var(--text-xs);
		font-weight: 700;
		color: var(--accent-text);
		box-shadow: inset 0 0 0 1px var(--accent);
		background: var(--bg);
	}
	.steps > li::after {
		content: '';
		position: absolute;
		left: 12px;
		top: 28px;
		bottom: 4px;
		width: 1px;
		background: var(--border);
	}
	.steps > li:last-child::after {
		display: none;
	}
	.steps > li:last-child {
		padding-bottom: 0;
	}
	.steps > li.done::before {
		content: '✓';
		color: var(--accent-on);
		background: var(--success);
		box-shadow: none;
	}
	.steps p {
		color: var(--fg-2);
		font-size: var(--text-sm);
	}
	.steps p.small {
		font-size: var(--text-xs);
	}
	.rec {
		display: grid;
		grid-template-columns: minmax(0, 1.2fr) auto minmax(0, 1.6fr);
		gap: var(--space-3);
	}
	.type {
		display: grid;
		gap: var(--space-1);
		align-content: start;
	}
	.tl {
		font-family: var(--font-mono);
		font-size: var(--text-xs);
		color: var(--fg-quiet);
	}
	.tv {
		min-height: 32px;
		display: flex;
		align-items: center;
		padding: 0 var(--space-3);
		font-family: var(--font-mono);
		font-size: var(--text-sm);
		font-weight: 600;
		border: 1px solid var(--border-soft);
		border-radius: var(--radius-sm);
	}
	@media (max-width: 640px) {
		.rec,
		.addr-fields {
			grid-template-columns: minmax(0, 1fr);
		}
		.steps > li {
			padding-left: 38px;
		}
		.route {
			flex-direction: column;
			align-items: flex-start;
		}
		.arrow {
			transform: rotate(90deg);
			padding-left: var(--space-2);
		}
	}
</style>
