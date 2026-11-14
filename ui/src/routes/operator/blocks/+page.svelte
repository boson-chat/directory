<script lang="ts">
	import { directory } from '$lib/api';
	import type { Block } from '$lib/types';
	import { toasts, errorText } from '$lib/toasts.svelte';
	import State from '$lib/components/State.svelte';
	import Confirm from '$lib/components/Confirm.svelte';
	import Notice from '$lib/components/Notice.svelte';
	import Time from '$lib/components/Time.svelte';

	let blocks = $state<Block[] | null>(null);
	let error = $state('');
	let pattern = $state('');
	let reason = $state('');
	let formError = $state('');
	let busy = $state(false);

	async function load() {
		error = '';
		try {
			blocks = (await directory.blocks()).blocks;
		} catch (e) {
			error = errorText(e);
		}
	}
	$effect(() => {
		load();
	});

	const valid = /^(\*\.)?([a-z0-9-]+\.)+[a-z0-9-]+$/;
	async function add(e: SubmitEvent) {
		e.preventDefault();
		const p = pattern.trim().toLowerCase().replace(/\.$/, '');
		formError = '';
		if (!valid.test(p)) {
			formError = 'Use a hostname (irc.example.org) or a suffix (*.example.org).';
			return;
		}
		busy = true;
		try {
			const { block: b, blocked_network_ids: hit } = await directory.addBlock(p, reason.trim());
			blocks = [b, ...(blocks ?? [])];
			pattern = '';
			reason = '';
			toasts.push(
				`Blocked ${p}.` +
					(hit?.length ? ` ${hit.length} listing${hit.length === 1 ? ' is' : 's are'} now hidden.` : '')
			);
		} catch (err) {
			formError = errorText(err);
		} finally {
			busy = false;
		}
	}
	async function remove(b: Block) {
		try {
			await directory.removeBlock(b.id);
			blocks = blocks!.filter((x) => x.id !== b.id);
			toasts.push(`Unblocked ${b.pattern}.`);
		} catch (err) {
			toasts.push(errorText(err), 'bad');
			throw err;
		}
	}
</script>

<svelte:head><title>Blocks · Operator · boson directory</title></svelte:head>

<section aria-labelledby="add-h">
	<div class="section-head">
		<h2 id="add-h">Block a hostname</h2>
		<p>Blocks hide listings registered here and imported from peers, and stop new registrations.</p>
	</div>
	<form class="add" onsubmit={add} novalidate>
		<label class="field">
			<span>Hostname or suffix</span>
			<input
				type="text"
				bind:value={pattern}
				placeholder="*.example.org"
				spellcheck="false"
				autocapitalize="none"
				aria-invalid={!!formError || undefined}
			/>
		</label>
		<label class="field">
			<span>Reason</span>
			<input type="text" bind:value={reason} placeholder="Shown in the audit log" maxlength="200" />
		</label>
		<button class="btn btn-primary" type="submit" disabled={busy || !pattern.trim()}
			>{busy ? 'Blocking…' : 'Block'}</button
		>
	</form>
	{#if formError}<div class="ferr"><Notice>{formError}</Notice></div>{/if}
</section>

<section class="section" aria-labelledby="list-h">
	<div class="section-head">
		<h2 id="list-h">Blocked</h2>
	</div>
	{#if error}
		<State kind="error">
			{error}
			{#snippet action()}<button class="btn btn-small" onclick={load}>Try again</button>{/snippet}
		</State>
	{:else if !blocks}
		<State kind="loading" title="Loading blocks…" />
	{:else if !blocks.length}
		<State kind="empty" title="Nothing is blocked">Every verified listing is shown.</State>
	{:else}
		<div class="table-wrap">
			<table>
				<thead>
					<tr><th>Pattern</th><th>Reason</th><th>Added</th><th><span class="sr-only">Actions</span></th></tr>
				</thead>
				<tbody>
					{#each blocks as b (b.id)}
						<tr>
							<td class="mono">{b.pattern}</td>
							<td>{b.reason || '—'}</td>
							<td class="when"
								><Time at={b.created_at} />{#if b.created_by}<span class="muted">
										by {b.created_by}</span
									>{/if}</td
							>
							<td class="act">
								<Confirm title="Unblock {b.pattern}?" action="Unblock" onconfirm={() => remove(b)}>
									{#snippet trigger(open)}<button class="btn btn-small btn-ghost" onclick={open}
											>Unblock</button
										>{/snippet}
									Matching listings come back once they pass their next DNS check.
								</Confirm>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{/if}
</section>

<style>
	.add {
		display: grid;
		grid-template-columns: minmax(0, 1fr) minmax(0, 1.4fr) auto;
		gap: var(--space-3);
		align-items: end;
		max-width: 860px;
	}
	.add .btn {
		min-height: 38px;
	}
	.ferr {
		margin-top: var(--space-3);
		max-width: 860px;
	}
	.when {
		white-space: nowrap;
		font-family: var(--font-mono);
		font-size: var(--text-xs);
	}
	.act {
		text-align: right;
	}
	@media (max-width: 640px) {
		.add {
			grid-template-columns: minmax(0, 1fr);
		}
	}
</style>
