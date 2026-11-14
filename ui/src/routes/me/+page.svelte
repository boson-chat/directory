<script lang="ts">
	import { directory } from '$lib/api';
	import type { Listing, Network } from '$lib/types';
	import { address, tokenExpired } from '$lib/links';
	import { errorText } from '$lib/toasts.svelte';
	import State from '$lib/components/State.svelte';
	import StatusTag from '$lib/components/StatusTag.svelte';
	import Health from '$lib/components/Health.svelte';
	import NetworkIcon from '$lib/components/NetworkIcon.svelte';
	import Time from '$lib/components/Time.svelte';

	let list = $state<Listing[] | null>(null);
	let error = $state('');

	async function load() {
		error = '';
		try {
			list = (await directory.mine()).listings;
		} catch (e) {
			error = errorText(e);
		}
	}
	$effect(() => {
		load();
	});

	function next(l: Listing): string {
		const n: Network = l.network;
		if (n.status === 'verified') return 'Listed. The directory re-checks the TXT record daily.';
		if (n.status === 'lapsed') return 'Hidden: the TXT record went missing. Put it back and check again.';
		if (n.status === 'blocked') return 'Blocked by the directory operator.';
		if (tokenExpired(l)) return 'The DNS record expired before it was checked. Issue a new one.';
		return 'Waiting for the TXT record. Add it, then check.';
	}
	let order: Record<string, number> = { pending: 0, lapsed: 1, verified: 2, blocked: 3 };
	let sorted = $derived(
		list ? [...list].sort((a, b) => order[a.network.status] - order[b.network.status]) : []
	);
</script>

<svelte:head><title>My networks · boson directory</title></svelte:head>

<div class="page-head">
	<div>
		<h1>My networks</h1>
		<p>
			Networks you’ve listed with your boson account. Listings from a boson stack are managed in its admin
			panel.
		</p>
	</div>
	<a class="btn btn-primary" href="/me/new">List a network</a>
</div>

{#if error}
	<State kind="error">
		{error}
		{#snippet action()}<button class="btn btn-small" onclick={load}>Try again</button>{/snippet}
	</State>
{:else if !list}
	<State kind="loading" title="Loading your networks…" />
{:else if !list.length}
	<State kind="empty" title="You haven’t listed a network">
		List one in two steps: describe it, then prove you run its hostname with a DNS TXT record.
		{#snippet action()}<a class="btn btn-small btn-primary" href="/me/new">List a network</a>{/snippet}
	</State>
{:else}
	<ul class="owned">
		{#each sorted as l (l.network.id)}
			{@const n = l.network}
			<li data-s={n.status}>
				<NetworkIcon name={n.name} url={n.icon_url} />
				<div class="what">
					<div class="line">
						<a class="name" href="/me/{encodeURIComponent(n.id)}">{n.name}</a>
						<StatusTag status={n.status} />
					</div>
					<span class="addr">{address(n)}</span>
					<span class="next">{next(l)}</span>
				</div>
				<div class="side">
					{#if n.status === 'verified'}
						<Health status={n.health_status} users={n.user_count} />
						<span class="muted when">Checked <Time at={n.last_checked_at ?? n.last_verified_at} /></span>
					{:else}
						<span class="muted when">Added <Time at={n.created_at} /></span>
					{/if}
					<a
						class="btn btn-small"
						class:btn-primary={n.status !== 'verified'}
						href="/me/{encodeURIComponent(n.id)}">{n.status === 'verified' ? 'Manage' : 'Finish setup'}</a
					>
				</div>
			</li>
		{/each}
	</ul>
{/if}

<style>
	.owned {
		list-style: none;
		margin: 0;
		padding: 0;
		border: 1px solid var(--border);
		border-radius: var(--radius-sm);
		background: var(--surface);
	}
	.owned li {
		display: flex;
		align-items: center;
		gap: var(--space-4);
		padding: var(--space-4) var(--space-5);
		border-bottom: 1px solid var(--border-soft);
	}
	.owned li:last-child {
		border-bottom: 0;
	}
	.owned li[data-s='pending'],
	.owned li[data-s='lapsed'] {
		box-shadow: inset 3px 0 0 var(--warn);
	}
	.what {
		flex: 1;
		min-width: 0;
		display: grid;
		gap: 2px;
	}
	.line {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: var(--space-2) var(--space-3);
	}
	.name {
		font-family: var(--font-display);
		font-weight: 700;
		color: var(--fg);
		text-decoration: none;
	}
	.name:hover {
		color: var(--accent-text);
	}
	.addr {
		font-family: var(--font-mono);
		font-size: var(--text-xs);
		color: var(--fg-quiet);
	}
	.next {
		font-size: var(--text-sm);
		color: var(--fg-2);
		margin-top: var(--space-1);
	}
	.side {
		display: grid;
		justify-items: end;
		gap: var(--space-2);
		flex: none;
	}
	.when {
		font-family: var(--font-mono);
		font-size: var(--text-xs);
	}
	@media (max-width: 640px) {
		.owned li {
			flex-wrap: wrap;
			align-items: flex-start;
			padding: var(--space-4);
		}
		.what {
			flex-basis: calc(100% - 64px);
		}
		.side {
			width: 100%;
			display: flex;
			flex-wrap: wrap;
			align-items: center;
			gap: var(--space-2) var(--space-4);
		}
		.side .btn {
			margin-left: auto;
		}
	}
</style>
