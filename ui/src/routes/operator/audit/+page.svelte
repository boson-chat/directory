<script lang="ts">
	import { directory } from '$lib/api';
	import type { AuditEntry } from '$lib/types';
	import { errorText } from '$lib/toasts.svelte';
	import State from '$lib/components/State.svelte';
	import Time from '$lib/components/Time.svelte';

	let entries = $state<AuditEntry[]>([]);
	let cursor = $state<string | null>(null);
	let loading = $state(true);
	let error = $state('');

	async function load(more = false) {
		loading = true;
		error = '';
		try {
			const r = await directory.audit(more ? cursor : null);
			entries = more ? [...entries, ...r.entries] : r.entries;
			cursor = r.next_cursor;
		} catch (e) {
			error = errorText(e);
		} finally {
			loading = false;
		}
	}
	$effect(() => {
		load();
	});

	const names: Record<string, string> = {
		'network.create': 'Registered a listing',
		'network.update': 'Edited a listing',
		'network.delete': 'Deleted a listing',
		'network.verify': 'Checked a listing’s DNS record',
		'network.regenerate_token': 'Issued a new DNS record',
		'network.replace_expired': 'Took over an expired registration',
		'network.replace_imported': 'Replaced an imported listing',
		'network.set_icon': 'Changed an icon',
		'network.clear_icon': 'Removed an icon',
		'network.set_banner': 'Changed a banner',
		'network.clear_banner': 'Removed a banner',
		'network.block': 'Blocked a listing',
		'network.unblock': 'Unblocked a listing',
		'network.recheck': 'Re-checked a listing',
		'block.add': 'Blocked a hostname',
		'block.remove': 'Unblocked a hostname',
		'peer.add': 'Added a peer',
		'peer.delete': 'Removed a peer',
		'peer.enable': 'Turned a peer on',
		'peer.disable': 'Turned a peer off',
		'peer.rename': 'Renamed a peer',
		'peer.sync': 'Synced a peer',
		'peer.seed': 'Added a peer from the environment',
		'peer.builtin_create': 'Added the main directory',
		'peer.builtin_off': 'Switched the main directory off',
		'peer.builtin_repoint': 'Pointed the main directory elsewhere'
	};
	const label = (a: string) => names[a] ?? a.replace(/[._]/g, ' ');
	const tone = (a: string) =>
		a === 'network.block' || a === 'block.add' || a === 'network.delete' || a === 'peer.delete'
			? 'bad'
			: a.startsWith('peer.')
				? 'peer'
				: 'plain';
	const actor = (e: AuditEntry) =>
		e.actor?.name || (e.actor?.kind === 'system' ? 'system' : e.actor?.id) || 'system';
	const target = (e: AuditEntry) => e.target?.hostname || e.target?.network_id || '';
	function meta(m: AuditEntry['meta']): string {
		if (!m) return '';
		return Object.entries(m)
			.map(([k, v]) => `${k.replaceAll('_', ' ')}: ${typeof v === 'object' ? JSON.stringify(v) : String(v)}`)
			.join(', ');
	}
</script>

<svelte:head><title>Audit log · Operator · boson directory</title></svelte:head>

{#if error && !entries.length}
	<State kind="error">
		{error}
		{#snippet action()}<button class="btn btn-small" onclick={() => load()}>Try again</button>{/snippet}
	</State>
{:else if loading && !entries.length}
	<State kind="loading" title="Loading the audit log…" />
{:else if !entries.length}
	<State kind="empty" title="Nothing has happened yet">Operator and owner actions show up here.</State>
{:else}
	<ol class="log">
		{#each entries as e (e.id)}
			<li data-t={tone(e.action)}>
				<span class="mark" aria-hidden="true"></span>
				<div class="what">
					<span class="act">{label(e.action)}</span>
					{#if target(e)}<span class="target mono">{target(e)}</span>{/if}
					{#if e.meta}<span class="meta">{meta(e.meta)}</span>{/if}
				</div>
				<div class="who">
					<span class="mono">{actor(e)}</span>
					<Time at={e.at} />
				</div>
			</li>
		{/each}
	</ol>
	{#if cursor}
		<div class="more">
			<button class="btn" onclick={() => load(true)} disabled={loading}
				>{loading ? 'Loading…' : 'Older entries'}</button
			>
		</div>
	{/if}
{/if}

<style>
	.log {
		list-style: none;
		margin: 0;
		padding: 0;
		border: 1px solid var(--border);
		border-radius: var(--radius-sm);
		background: var(--surface);
	}
	.log li {
		display: flex;
		align-items: flex-start;
		gap: var(--space-3);
		padding: var(--space-3) var(--space-4);
		border-bottom: 1px solid var(--border-soft);
	}
	.log li:last-child {
		border-bottom: 0;
	}
	.mark {
		flex: none;
		width: 8px;
		height: 8px;
		margin-top: 6px;
		border-radius: 50%;
		background: var(--fg-3);
	}
	[data-t='bad'] .mark {
		background: var(--danger);
	}
	[data-t='peer'] .mark {
		background: var(--info);
	}
	.what {
		flex: 1;
		min-width: 0;
		display: flex;
		flex-wrap: wrap;
		gap: 2px var(--space-3);
		align-items: baseline;
	}
	.act {
		font-weight: 600;
		font-size: var(--text-sm);
	}
	.target {
		font-size: var(--text-xs);
		color: var(--fg-2);
		overflow-wrap: anywhere;
	}
	.meta {
		flex-basis: 100%;
		font-family: var(--font-mono);
		font-size: var(--text-xs);
		color: var(--fg-quiet);
		overflow-wrap: anywhere;
	}
	.who {
		display: grid;
		justify-items: end;
		gap: 2px;
		font-size: var(--text-xs);
		color: var(--fg-quiet);
		font-family: var(--font-mono);
		white-space: nowrap;
	}
	.more {
		display: flex;
		justify-content: center;
		margin-top: var(--space-6);
	}
	@media (max-width: 560px) {
		.log li {
			flex-wrap: wrap;
		}
		.who {
			flex-basis: 100%;
			display: flex;
			gap: var(--space-3);
			padding-left: 20px;
		}
	}
</style>
