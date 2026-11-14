<script lang="ts">
	import { untrack } from 'svelte';
	import { directory } from '$lib/api';
	import type { NetworkStatus, Network } from '$lib/types';
	import { address, originLabel } from '$lib/links';
	import { num } from '$lib/format';
	import type { DropdownOption } from '$lib/dropdown';
	import { toasts, errorText } from '$lib/toasts.svelte';
	import Dropdown from '$lib/components/Dropdown.svelte';
	import State from '$lib/components/State.svelte';
	import StatusTag from '$lib/components/StatusTag.svelte';
	import Confirm from '$lib/components/Confirm.svelte';
	import NetworkIcon from '$lib/components/NetworkIcon.svelte';
	import Time from '$lib/components/Time.svelte';

	let q = $state('');
	let status = $state<NetworkStatus | ''>('');
	let rows = $state<Network[]>([]);
	let cursor = $state<string | null>(null);
	let total = $state<number | null>(null);
	let loading = $state(true);
	let error = $state('');

	const statuses: DropdownOption<NetworkStatus | ''>[] = [
		{ value: '', label: 'Any status' },
		{ value: 'verified', label: 'Verified' },
		{ value: 'pending', label: 'Pending DNS' },
		{ value: 'lapsed', label: 'Lapsed' },
		{ value: 'blocked', label: 'Blocked' }
	];

	async function load(more = false) {
		loading = true;
		error = '';
		try {
			const r = await directory.listings({ q: q.trim(), status, cursor: more ? cursor : null });
			rows = more ? [...rows, ...r.networks] : r.networks;
			cursor = r.next_cursor;
			total = r.total_estimate ?? null;
		} catch (e) {
			error = errorText(e);
		} finally {
			loading = false;
		}
	}
	let t: ReturnType<typeof setTimeout>;
	$effect(() => {
		void status;
		untrack(() => load());
	});
	function onq() {
		clearTimeout(t);
		t = setTimeout(() => load(), 250);
	}

	async function act(n: Network, action: 'block' | 'unblock' | 'recheck') {
		try {
			const { network: up } = await directory.moderate(n.id, action);
			rows = rows.map((x) => (x.id === n.id ? up : x));
			toasts.push(
				action === 'block'
					? `${n.name} is hidden.`
					: action === 'unblock'
						? `${n.name} is unblocked.`
						: `Re-checking ${n.hostname}’s DNS record.`
			);
		} catch (e) {
			toasts.push(errorText(e), 'bad');
			throw e;
		}
	}
	async function remove(n: Network) {
		try {
			await directory.adminRemove(n.id);
			rows = rows.filter((x) => x.id !== n.id);
			toasts.push(`${n.name} was deleted.`);
		} catch (e) {
			toasts.push(errorText(e), 'bad');
			throw e;
		}
	}
</script>

<svelte:head><title>Listings · Operator · boson directory</title></svelte:head>

<div class="bar">
	<label class="field grow">
		<span>Search</span>
		<input type="search" bind:value={q} oninput={onq} placeholder="Name or hostname" spellcheck="false" />
	</label>
	<div class="field">
		<span id="st-l">Status</span>
		<Dropdown options={statuses} bind:value={status} labelledby="st-l" />
	</div>
	{#if total !== null}<p class="count">{num(total)} listings</p>{/if}
</div>

{#if error && !rows.length}
	<State kind="error">
		{error}
		{#snippet action()}<button class="btn btn-small" onclick={() => load()}>Try again</button>{/snippet}
	</State>
{:else if loading && !rows.length}
	<State kind="loading" title="Loading listings…" />
{:else if !rows.length}
	<State kind="empty" title="No listings match">Try another status or search.</State>
{:else}
	<div class="table-wrap">
		<table>
			<thead>
				<tr>
					<th>Network</th>
					<th>Status</th>
					<th>Source</th>
					<th>DNS last seen</th>
					<th><span class="sr-only">Actions</span></th>
				</tr>
			</thead>
			<tbody>
				{#each rows as n (n.id)}
					<tr>
						<td>
							<div class="net">
								<NetworkIcon name={n.name} url={n.icon_url} size={32} />
								<div>
									<a href="/n/{encodeURIComponent(n.id)}">{n.name}</a>
									<span class="addr">{address(n)}</span>
								</div>
							</div>
						</td>
						<td><StatusTag status={n.status} /></td>
						<td class="mono src">{originLabel(n)}</td>
						<td class="mono when"><Time at={n.last_verified_at} empty="never" /></td>
						<td class="acts">
							<button class="btn btn-small btn-ghost" onclick={() => act(n, 'recheck').catch(() => {})}
								>Re-check</button
							>
							{#if n.status === 'blocked'}
								<button class="btn btn-small" onclick={() => act(n, 'unblock').catch(() => {})}
									>Unblock</button
								>
							{:else}
								<Confirm
									title="Hide {n.name}?"
									action="Block listing"
									danger
									onconfirm={() => act(n, 'block')}
								>
									{#snippet trigger(open)}<button class="btn btn-small" onclick={open}>Block</button
										>{/snippet}
									It disappears from this directory until you unblock it. To block the hostname for good, including
									re-registrations and peers, add it on the Blocks tab.
								</Confirm>
							{/if}
							<Confirm
								title="Delete {n.name}?"
								action="Delete listing"
								danger
								typed={n.hostname}
								onconfirm={() => remove(n)}
							>
								{#snippet trigger(open)}<button class="btn btn-small btn-danger" onclick={open}>Delete</button
									>{/snippet}
								{n.origin === 'local'
									? 'The owner can register it again.'
									: 'An imported listing comes back at the next sync unless the hostname is blocked.'}
							</Confirm>
						</td>
					</tr>
				{/each}
			</tbody>
		</table>
	</div>
	{#if cursor}
		<div class="more">
			<button class="btn" onclick={() => load(true)} disabled={loading}
				>{loading ? 'Loading…' : 'Load more'}</button
			>
		</div>
	{/if}
{/if}

<style>
	.bar {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-end;
		gap: var(--space-3) var(--space-4);
		margin-bottom: var(--space-5);
	}
	.bar .field {
		min-width: 180px;
	}
	.grow {
		flex: 1;
		max-width: 420px;
	}
	.count {
		margin-left: auto;
		font-family: var(--font-mono);
		font-size: var(--text-xs);
		color: var(--fg-quiet);
		padding-bottom: var(--space-2);
	}
	.net {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		min-width: 220px;
	}
	.net div {
		display: grid;
	}
	.net a {
		font-weight: 600;
	}
	.addr {
		font-family: var(--font-mono);
		font-size: var(--text-xs);
		color: var(--fg-quiet);
	}
	.src,
	.when {
		font-size: var(--text-xs);
		white-space: nowrap;
	}
	.acts {
		white-space: nowrap;
		text-align: right;
	}
	.acts > :global(*) {
		margin-left: var(--space-1);
	}
	.more {
		display: flex;
		justify-content: center;
		margin-top: var(--space-6);
	}
</style>
