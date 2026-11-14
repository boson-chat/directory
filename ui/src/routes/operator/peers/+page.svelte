<script lang="ts">
	import { directory, ApiError } from '$lib/api';
	import type { Peer, PeerCheck } from '$lib/types';
	import { num } from '$lib/format';
	import { toasts, errorText } from '$lib/toasts.svelte';
	import State from '$lib/components/State.svelte';
	import Toggle from '$lib/components/Toggle.svelte';
	import Confirm from '$lib/components/Confirm.svelte';
	import Notice from '$lib/components/Notice.svelte';
	import Time from '$lib/components/Time.svelte';

	let peers = $state<Peer[] | null>(null);
	let error = $state('');
	let url = $state('');
	let checked = $state<PeerCheck | null>(null);
	let checkError = $state('');
	let checking = $state(false);
	let adding = $state(false);
	let syncing = $state<Record<string, boolean>>({});

	async function load() {
		error = '';
		try {
			peers = (await directory.peers()).peers;
		} catch (e) {
			error = errorText(e);
		}
	}
	$effect(() => {
		load();
	});

	let builtin = $derived(peers?.find((p) => p.builtin) ?? null);
	let others = $derived(peers?.filter((p) => !p.builtin) ?? []);

	function normalised(u: string): string {
		const t = u.trim().replace(/\/+$/, '');
		return /^[a-z]+:\/\//i.test(t) ? t : 'https://' + t;
	}

	async function check(e: SubmitEvent) {
		e.preventDefault();
		checked = null;
		checkError = '';
		if (!url.trim()) return;
		checking = true;
		try {
			checked = await directory.checkPeer(normalised(url));
		} catch (err) {
			checkError = peerError(err);
		} finally {
			checking = false;
		}
	}
	async function add() {
		if (!checked) return;
		adding = true;
		try {
			const { peer: p } = await directory.addPeer(checked.url);
			peers = [...(peers ?? []), p];
			toasts.push(`Importing from ${checked.well_known.name}. The first sync starts now.`);
			checked = null;
			url = '';
		} catch (err) {
			checkError = peerError(err);
		} finally {
			adding = false;
		}
	}
	async function setEnabled(p: Peer, enabled: boolean) {
		try {
			const { peer: up } = await directory.setPeer(p.id, enabled);
			peers = peers!.map((x) => (x.id === p.id ? up : x));
			toasts.push(
				enabled
					? `Importing from ${p.name} again.`
					: `Stopped importing from ${p.name}. Its listings are hidden.`
			);
		} catch (err) {
			peers = peers!.map((x) => (x.id === p.id ? { ...x, enabled: !enabled } : x));
			toasts.push(errorText(err), 'bad');
		}
	}
	async function sync(p: Peer) {
		syncing[p.id] = true;
		try {
			const { peer: up } = await directory.syncPeer(p.id);
			peers = peers!.map((x) => (x.id === p.id ? up : x));
			if (up.last_error) toasts.push(`Sync with ${p.name} failed: ${up.last_error}`, 'bad');
			else toasts.push(`Synced with ${p.name}.`);
		} catch (err) {
			toasts.push(errorText(err), 'bad');
		} finally {
			syncing[p.id] = false;
		}
	}
	async function remove(p: Peer) {
		try {
			await directory.removePeer(p.id);
			peers = peers!.filter((x) => x.id !== p.id);
			toasts.push(`Removed ${p.name}. Its listings disappear from this directory.`);
		} catch (err) {
			toasts.push(errorText(err), 'bad');
			throw err;
		}
	}
	function peerError(e: unknown): string {
		if (e instanceof ApiError && e.code === 'self_peer') return 'That’s this directory. Add another one.';
		if (e instanceof ApiError && e.code === 'peer_exists') return 'That directory is already a peer.';
		if (e instanceof ApiError && e.code === 'peer_unreachable')
			return `Couldn’t read its /.well-known/boson-directory.json: ${e.message}`;
		return errorText(e);
	}
	const host = (u: string) => u.replace(/^https?:\/\//, '');
</script>

<svelte:head><title>Peers · Operator · boson directory</title></svelte:head>

{#snippet peerRow(p: Peer)}
	<li class="peer" class:off={!p.enabled}>
		<div class="what">
			<div class="line">
				<span class="name">{p.name}</span>
				{#if p.builtin}<span class="tag tag-accent">Main directory</span>{/if}
				{#if !p.enabled}<span class="tag">Off</span>{/if}
			</div>
			<a class="url" href={p.url} rel="noopener noreferrer" target="_blank">{host(p.url)}</a>
			<div class="stats">
				<span><strong>{num(p.listing_count)}</strong> listings</span>
				<span
					>Last sync {#if p.last_sync_at}<Time at={p.last_sync_at} />{:else}never{/if}</span
				>
				{#if p.cursor !== null && p.cursor !== undefined}<span>Feed position {p.cursor}</span>{/if}
			</div>
			{#if p.last_error}
				<p class="perr" role="note"><span class="sr-only">Last error: </span>{p.last_error}</p>
			{/if}
		</div>
		<div class="ctl">
			<Toggle
				label={p.enabled ? 'Importing' : 'Not importing'}
				checked={p.enabled}
				onchange={(v) => setEnabled(p, v)}
			/>
			<div class="btns">
				<button class="btn btn-small" onclick={() => sync(p)} disabled={!p.enabled || syncing[p.id]}
					>{syncing[p.id] ? 'Syncing…' : 'Sync now'}</button
				>
				{#if !p.builtin}
					<Confirm title="Remove {p.name}?" action="Remove peer" danger onconfirm={() => remove(p)}>
						{#snippet trigger(open)}<button class="btn btn-small btn-ghost" onclick={open}>Remove</button
							>{/snippet}
						Its {num(p.listing_count)} imported listings disappear from this directory. Listings registered here
						aren’t affected.
					</Confirm>
				{/if}
			</div>
		</div>
	</li>
{/snippet}

{#if error}
	<State kind="error">
		{error}
		{#snippet action()}<button class="btn btn-small" onclick={load}>Try again</button>{/snippet}
	</State>
{:else if !peers}
	<State kind="loading" title="Loading peers…" />
{:else}
	<section aria-labelledby="main-h">
		<div class="section-head">
			<h2 id="main-h">The boson main directory</h2>
		</div>
		<div class="explain">
			<p>
				Every directory imports <code>directory.boson.chat</code> by default, so people browsing here also
				find the networks listed there. Imported listings only count as verified after this directory checks
				each network’s own <code>_boson</code> TXT record itself.
			</p>
			<p>
				Switch it off to run an independent directory. The setting sticks across restarts; the
				<code>DIRECTORY_INCLUDE_MAIN=false</code> environment variable does the same at deploy time.
			</p>
		</div>
		{#if builtin}
			<ul class="peers">{@render peerRow(builtin)}</ul>
		{:else}
			<State kind="off" title="This directory doesn’t import the main directory">
				Either this is directory.boson.chat itself, or <code>DIRECTORY_INCLUDE_MAIN</code> is false.
			</State>
		{/if}
	</section>

	<section class="section" aria-labelledby="peers-h">
		<div class="section-head">
			<h2 id="peers-h">Other directories</h2>
			<p>Import listings from any directory that publishes <code>/.well-known/boson-directory.json</code>.</p>
		</div>
		<form class="add" onsubmit={check} novalidate>
			<label class="field">
				<span>Directory address</span>
				<div class="row">
					<input
						type="url"
						bind:value={url}
						placeholder="https://directory.example.org"
						spellcheck="false"
						autocapitalize="none"
						oninput={() => {
							checked = null;
							checkError = '';
						}}
					/>
					<button class="btn" type="submit" disabled={checking || !url.trim()}
						>{checking ? 'Checking…' : 'Check'}</button
					>
				</div>
			</label>
			{#if checkError}<Notice>{checkError}</Notice>{/if}
			{#if checked}
				<div class="wk" aria-live="polite">
					<dl>
						<div>
							<dt>Name</dt>
							<dd>{checked.well_known.name}</dd>
						</div>
						<div>
							<dt>API</dt>
							<dd class="mono">{checked.well_known.api}</dd>
						</div>
						<div>
							<dt>Version</dt>
							<dd class="mono">{checked.well_known.version}</dd>
						</div>
						{#if checked.well_known.operator}<div>
								<dt>Operator</dt>
								<dd>{checked.well_known.operator}</dd>
							</div>{/if}
						{#if checked.well_known.contact}<div>
								<dt>Contact</dt>
								<dd class="mono">{checked.well_known.contact}</dd>
							</div>{/if}
						<div>
							<dt>Federation</dt>
							<dd>{checked.well_known.federation ? 'Publishes a feed' : 'No feed; can’t be imported'}</dd>
						</div>
					</dl>
					<button
						class="btn btn-primary"
						type="button"
						onclick={add}
						disabled={adding || !checked.well_known.federation}
						>{adding ? 'Adding…' : `Import from ${checked.well_known.name}`}</button
					>
				</div>
			{/if}
		</form>

		{#if others.length}
			<ul class="peers">
				{#each others as p (p.id)}{@render peerRow(p)}{/each}
			</ul>
		{:else}
			<State kind="empty" title="No other directories yet">
				Add a directory’s address above to show its networks here too.
			</State>
		{/if}
	</section>
{/if}

<style>
	.explain {
		display: grid;
		gap: var(--space-2);
		max-width: 72ch;
		margin-bottom: var(--space-4);
		color: var(--fg-2);
		font-size: var(--text-sm);
	}
	.peers {
		list-style: none;
		margin: var(--space-4) 0 0;
		padding: 0;
		border: 1px solid var(--border);
		border-radius: var(--radius-sm);
		background: var(--surface);
	}
	.peer {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-4) var(--space-6);
		justify-content: space-between;
		padding: var(--space-4) var(--space-5);
		border-bottom: 1px solid var(--border-soft);
	}
	.peer:last-child {
		border-bottom: 0;
	}
	.peer.off .name,
	.peer.off .stats {
		color: var(--fg-quiet);
	}
	.what {
		flex: 1;
		min-width: min(100%, 280px);
		display: grid;
		gap: 2px;
	}
	.line {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
	}
	.name {
		font-family: var(--font-display);
		font-weight: 700;
	}
	.url {
		font-family: var(--font-mono);
		font-size: var(--text-xs);
		color: var(--fg-quiet);
		width: fit-content;
	}
	.stats {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1) var(--space-4);
		margin-top: var(--space-2);
		font-family: var(--font-mono);
		font-size: var(--text-xs);
		color: var(--fg-2);
	}
	.stats strong {
		color: var(--fg);
	}
	.perr {
		margin-top: var(--space-2);
		font-family: var(--font-mono);
		font-size: var(--text-xs);
		color: var(--danger-text);
		overflow-wrap: anywhere;
		padding-left: var(--space-3);
		border-left: 2px solid var(--danger);
	}
	.ctl {
		display: grid;
		gap: var(--space-3);
		justify-items: end;
		align-content: start;
		min-width: 200px;
	}
	.ctl :global(.toggle) {
		width: 100%;
	}
	.btns {
		display: flex;
		gap: var(--space-2);
	}
	.add {
		display: grid;
		gap: var(--space-3);
		max-width: 640px;
		margin-bottom: var(--space-4);
	}
	.row {
		display: flex;
		gap: var(--space-2);
	}
	.wk {
		display: grid;
		gap: var(--space-4);
		padding: var(--space-4) var(--space-5);
		border: 1px solid var(--border);
		border-left: 3px solid var(--info);
		border-radius: 0 var(--radius-sm) var(--radius-sm) 0;
		background: var(--surface);
	}
	.wk .btn {
		justify-self: start;
	}
	dl {
		margin: 0;
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(200px, 1fr));
		gap: var(--space-3) var(--space-4);
	}
	dt {
		font-family: var(--font-mono);
		font-size: var(--text-xs);
		color: var(--fg-quiet);
	}
	dd {
		margin: 0;
		font-size: var(--text-sm);
		overflow-wrap: anywhere;
	}
	@media (max-width: 640px) {
		.ctl {
			min-width: 0;
			width: 100%;
			justify-items: stretch;
		}
		.btns .btn {
			flex: 1;
		}
		.peer {
			padding: var(--space-4);
		}
	}
</style>
