<script lang="ts">
	import type { VerifyReport } from '$lib/types';

	/** What each public resolver saw for a DNS check. */
	let {
		report,
		okText = 'All three resolvers see the record.',
		hint = ''
	}: { report: VerifyReport; okText?: string; hint?: string } = $props();

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
	let matched = $derived(Object.values(report.results).filter((r) => r.outcome === 'match').length);
</script>

<div class="report" aria-live="polite">
	<p class="summary" class:ok={report.success}>
		{report.success ? okText : `${matched} of 3 resolvers see the record.`}
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
	{#if !report.success && hint}
		<p class="muted small">{hint}</p>
	{/if}
</div>

<style>
	.report {
		display: grid;
		gap: var(--space-2);
	}
	p {
		color: var(--fg-2);
		font-size: var(--text-sm);
	}
	p.small {
		font-size: var(--text-xs);
		line-height: 1.6;
	}
	p.summary {
		font-family: var(--font-mono);
		color: var(--warn-text);
	}
	p.summary.ok {
		color: var(--success-text);
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
</style>
