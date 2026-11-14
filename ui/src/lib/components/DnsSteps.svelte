<script lang="ts">
	import { onDestroy } from 'svelte';
	import { directory, ApiError } from '$lib/api';
	import type { Listing, VerifyReport, VerifyResponse } from '$lib/types';
	import { digCommand, recordName, recordValue, tokenExpired, tokenExpiry } from '$lib/links';
	import { datetime } from '$lib/format';
	import { toasts, errorText } from '$lib/toasts.svelte';
	import CopyField from './CopyField.svelte';
	import Confirm from './Confirm.svelte';
	import Notice from './Notice.svelte';

	/** The TXT record to publish and the check against three public resolvers. */
	let { listing = $bindable() }: { listing: Listing } = $props();
	let network = $derived(listing.network);

	let checking = $state(false);
	let report = $state<VerifyReport | null>(null);
	let problem = $state<{ kind: 'expired' | 'error'; text: string } | null>(null);
	let wait = $state(0);
	let timer: ReturnType<typeof setInterval> | undefined;
	onDestroy(() => clearInterval(timer));

	let expiry = $derived(tokenExpiry(listing));
	let expired = $derived(network.status === 'pending' && tokenExpired(listing));
	let token = $derived(listing.verification?.token ?? network.verification_token ?? '');
	let name = $derived(listing.verification?.record_name ?? recordName(network.hostname));
	let value = $derived(listing.verification?.record_value ?? recordValue(token));

	function cooldown(s: number) {
		wait = s;
		clearInterval(timer);
		timer = setInterval(() => {
			wait = Math.max(0, wait - 1);
			if (!wait) clearInterval(timer);
		}, 1000);
	}

	async function check() {
		checking = true;
		problem = null;
		try {
			const r = await directory.verify(network.id);
			report = r.report;
			listing = { network: r.network, verification: r.verification };
			toasts.push(`${r.network.hostname} is verified and listed.`);
		} catch (e) {
			if (
				e instanceof ApiError &&
				e.status === 409 &&
				e.data &&
				typeof e.data === 'object' &&
				'report' in e.data
			) {
				const r = e.data as VerifyResponse;
				report = r.report;
				if (r.network) listing = { network: r.network, verification: r.verification ?? listing.verification };
			} else if (e instanceof ApiError && e.status === 410) {
				report = null;
				problem = {
					kind: 'expired',
					text: 'This record expired before all three resolvers saw it. Issue a new one, update the TXT record, then check again.'
				};
			} else if (e instanceof ApiError && e.status === 429) {
				cooldown(e.retryAfter || 30);
				problem = {
					kind: 'error',
					text: 'DNS checks are limited. Give the record a moment to spread first.'
				};
			} else {
				problem = { kind: 'error', text: errorText(e) };
			}
		} finally {
			checking = false;
		}
	}

	async function regenerate() {
		try {
			listing = await directory.regenerate(network.id);
			report = null;
			problem = null;
			toasts.push('New record issued. Replace the old TXT value with it.');
		} catch (e) {
			toasts.push(errorText(e), 'bad');
			throw e;
		}
	}

	const resolverNames: Record<string, string> = {
		cloudflare: 'Cloudflare 1.1.1.1',
		google: 'Google 8.8.8.8',
		quad9: 'Quad9 9.9.9.9'
	};
	const outcome: Record<string, string> = {
		match: 'Sees the record',
		missing_record: 'No record yet',
		timeout: 'Timed out',
		error: 'Lookup failed'
	};
	let matched = $derived(
		report ? Object.values(report.results).filter((r) => r.outcome === 'match').length : 0
	);
	let sub = $derived(network.hostname.split('.').slice(0, -2).join('.'));
</script>

<ol class="steps">
	<li class="done">
		<h3>Submit the listing</h3>
		<p>Done. It stays hidden until the DNS check passes.</p>
	</li>
	<li>
		<h3>Add this TXT record at your DNS provider</h3>
		{#if network.status === 'lapsed'}
			<Notice tone="warn">
				The directory stopped seeing this record, so the listing is hidden. Put it back (or issue a new one)
				and check again.
			</Notice>
		{/if}
		{#if token}
			<div class="rec">
				<CopyField label="Name" value={name} />
				<div class="type"><span class="tl">Type</span><span class="tv">TXT</span></div>
				<CopyField label="Value" {value} />
			</div>
			<p class="muted small">
				{#if sub}Some DNS providers want only <code>_boson.{sub}</code> as the name, without the domain.{/if}
				A TTL of 300 seconds keeps the wait short.
				{#if expiry && network.status === 'pending'}
					{#if expired}<strong class="bad">This record expired {datetime(expiry.toISOString())}.</strong>
					{:else}Check it before {datetime(expiry.toISOString())}; after that you’ll need a new one.{/if}
				{/if}
			</p>
			<CopyField label="See what the world sees" value={digCommand(network.hostname)} />
		{:else}
			<p>No record has been issued. Issue one to continue.</p>
		{/if}
	</li>
	<li>
		<h3>Check it</h3>
		<p>The directory asks three public resolvers. All three must see the record the first time.</p>
		<div class="check">
			<button
				class="btn btn-primary btn-small"
				onclick={check}
				disabled={checking || wait > 0 || expired || !token}
				>{checking ? 'Checking…' : wait ? `Check again in ${wait}s` : 'Check DNS now'}</button
			>
			<Confirm title="Issue a new record?" action="Issue new record" onconfirm={regenerate}>
				{#snippet trigger(open)}
					<button class="btn btn-small" class:btn-ghost={!expired && token} onclick={open}>New record</button>
				{/snippet}
				The current value stops working. Replace the TXT record with the new one before you check.
			</Confirm>
		</div>
		{#if expired && !problem}
			<Notice tone="warn">This record expired. Issue a new one to try again.</Notice>
		{/if}
		{#if problem}
			<Notice tone={problem.kind === 'expired' ? 'warn' : 'bad'}>{problem.text}</Notice>
		{/if}
		{#if report}
			<div class="report" aria-live="polite">
				<p class="summary" class:ok={report.success}>
					{report.success
						? 'All three resolvers see the record. The listing is live.'
						: `${matched} of 3 resolvers see the record.`}
				</p>
				<ul class="resolvers">
					{#each Object.entries(report.results) as [k, r] (k)}
						<li data-o={r.outcome}>
							<span class="rname">{resolverNames[k] ?? k}</span>
							<span class="rout">{outcome[r.outcome] ?? r.outcome}</span>
							{#if r.detail}<span class="muted rdet">{r.detail}</span>{/if}
						</li>
					{/each}
				</ul>
				{#if !report.success}
					<p class="muted small">
						DNS changes can take a few minutes to reach every resolver. The directory also checks pending
						listings daily on its own.
					</p>
				{/if}
			</div>
		{/if}
	</li>
</ol>

<style>
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
		line-height: 1.6;
	}
	.steps p.summary {
		font-family: var(--font-mono);
		color: var(--warn-text);
	}
	.steps p.summary.ok {
		color: var(--success-text);
	}
	.rec {
		display: grid;
		grid-template-columns: minmax(0, 1.2fr) auto minmax(0, 1.6fr);
		gap: var(--space-3);
	}
	.bad {
		color: var(--danger-text);
		font-weight: 600;
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
	.check {
		display: flex;
		gap: var(--space-2);
		flex-wrap: wrap;
	}
	.report {
		display: grid;
		gap: var(--space-2);
	}
	.resolvers {
		list-style: none;
		margin: 0;
		padding: 0;
		border: 1px solid var(--border);
		border-radius: var(--radius-sm);
		background: var(--surface);
	}
	.resolvers li {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1) var(--space-4);
		align-items: center;
		padding: var(--space-2) var(--space-4);
		border-bottom: 1px solid var(--border-soft);
		font-family: var(--font-mono);
		font-size: var(--text-sm);
	}
	.resolvers li:last-child {
		border-bottom: 0;
	}
	.rname {
		min-width: 18ch;
	}
	.rout {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		color: var(--danger-text);
	}
	.rout::before {
		content: '';
		width: 6px;
		height: 6px;
		border-radius: 50%;
		background: var(--danger);
	}
	[data-o='match'] .rout {
		color: var(--success-text);
	}
	[data-o='match'] .rout::before {
		background: var(--success);
	}
	[data-o='timeout'] .rout {
		color: var(--warn-text);
	}
	[data-o='timeout'] .rout::before {
		background: var(--warn);
	}
	.rdet {
		font-size: var(--text-xs);
		overflow-wrap: anywhere;
	}
	@media (max-width: 640px) {
		.rec {
			grid-template-columns: minmax(0, 1fr);
		}
		.steps > li {
			padding-left: 38px;
		}
	}
</style>
