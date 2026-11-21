<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { directory, ApiError } from '$lib/api';
	import type { Listing, NetworkInput } from '$lib/types';
	import { address, joinUrl } from '$lib/links';
	import { fieldErrors } from '$lib/errors';
	import { toasts, errorText } from '$lib/toasts.svelte';
	import State from '$lib/components/State.svelte';
	import StatusTag from '$lib/components/StatusTag.svelte';
	import Health from '$lib/components/Health.svelte';
	import NetworkIcon from '$lib/components/NetworkIcon.svelte';
	import NetworkForm from '$lib/components/NetworkForm.svelte';
	import DnsSteps from '$lib/components/DnsSteps.svelte';
	import ImageField from '$lib/components/ImageField.svelte';
	import ThemeEditor from '$lib/components/ThemeEditor.svelte';
	import Confirm from '$lib/components/Confirm.svelte';
	import Time from '$lib/components/Time.svelte';

	let listing = $state<Listing | null>(null);
	let n = $derived(listing?.network ?? null);
	let error = $state<{ status: number; text: string } | null>(null);
	let formKey = $state(0);
	let local = $derived(n?.origin === 'local');

	async function load(id: string) {
		error = null;
		try {
			const got = await directory.get(id);
			// Only the listing's managers get its record and owner.
			if (!got.verification && !got.network.owner) {
				error = { status: 403, text: 'This listing belongs to another account.' };
				return;
			}
			listing = got;
		} catch (e) {
			error = { status: e instanceof ApiError ? e.status : 0, text: errorText(e) };
		}
	}
	$effect(() => {
		load(page.params.id ?? '');
	});

	async function save(v: NetworkInput) {
		if (!n) return;
		try {
			listing = await directory.update(n.id, {
				name: v.name,
				description: v.description,
				tags: v.tags,
				languages: v.languages,
				is_nsfw: v.is_nsfw,
				website_url: v.website_url,
				channels: v.channels,
				autojoin_channels: v.autojoin_channels
			});
			toasts.push('Listing saved.');
		} catch (e) {
			const f = fieldErrors(e);
			if (f) return f;
			toasts.push(errorText(e), 'bad');
		}
	}

	async function remove() {
		if (!n) return;
		try {
			await directory.remove(n.id);
			toasts.push(`${n.name} was removed from the directory.`);
			goto('/me');
		} catch (e) {
			toasts.push(errorText(e), 'bad');
			throw e;
		}
	}
</script>

<svelte:head><title>{n ? n.name : 'Listing'} · boson directory</title></svelte:head>

<p class="back"><a href="/me">My networks</a></p>

{#if error}
	{#if error.status === 404 || error.status === 403}
		<State kind="empty" title="No listing of yours here">
			{error.status === 403 ? error.text : 'It may have been deleted.'}
			{#snippet action()}<a class="btn btn-small" href="/me">My networks</a>{/snippet}
		</State>
	{:else}
		<State kind="error">
			{error.text}
			{#snippet action()}<button class="btn btn-small" onclick={() => load(page.params.id ?? '')}
					>Try again</button
				>{/snippet}
		</State>
	{/if}
{:else if !listing || !n}
	<State kind="loading" title="Loading the listing…" />
{:else}
	<header class="head">
		<NetworkIcon name={n.name} url={n.icon_url} size={64} />
		<div class="title">
			<h1>{n.name}</h1>
			<div class="sub">
				<span class="addr">{address(n)}</span>
				<StatusTag status={n.status} />
				{#if n.status === 'verified'}<Health status={n.health_status} users={n.user_count} />{/if}
			</div>
		</div>
		{#if n.status === 'verified'}
			<div class="acts">
				<a class="btn btn-small" href="/n/{encodeURIComponent(n.id)}">View public page</a>
				<a class="btn btn-small" href={joinUrl(n)}>Open in Boson</a>
			</div>
		{/if}
	</header>

	{#if !local}
		<div class="section">
			<State kind="off" title="Imported from {n.origin_name ?? 'another directory'}">
				Its owner edits it there; this directory re-checks its DNS record itself. As an operator you can
				moderate or delete it here.
			</State>
		</div>
	{:else if n.status === 'blocked'}
		<State kind="off" title="The directory operator blocked this listing">
			It isn’t shown to anyone. Contact the operator of this directory if you think that’s a mistake.
		</State>
	{:else if n.status !== 'verified'}
		<section class="section" aria-labelledby="dns-h">
			<div class="section-head">
				<h2 id="dns-h">Prove you run {n.hostname}</h2>
				{#if n.last_checked_at}<p>Last checked <Time at={n.last_checked_at} /></p>{/if}
			</div>
			<DnsSteps bind:listing />
		</section>
	{:else}
		<section class="section verified" aria-labelledby="ok-h">
			<h2 id="ok-h" class="sr-only">DNS record</h2>
			<p>
				The TXT record at <code>_boson.{n.hostname}</code> was last seen <Time at={n.last_verified_at} />.
				Keep it in place: the directory re-checks it daily and hides the listing after 14 days without it.
			</p>
		</section>
	{/if}

	{#if local}
		<section class="section" aria-labelledby="img-h">
			<div class="section-head">
				<h2 id="img-h">Images</h2>
				<p>Shown on the network’s card and page.</p>
			</div>
			<div class="imgs">
				<ImageField bind:listing which="icon" />
				<ImageField bind:listing which="banner" />
			</div>
		</section>

		<section class="section" aria-labelledby="theme-h">
			<div class="section-head">
				<h2 id="theme-h">Theme</h2>
				<p>
					Your network’s colours, logo and backgrounds: on its card and page here, and in Boson when people
					connect or sign in.
				</p>
			</div>
			{#key n.id}
				<ThemeEditor bind:listing />
			{/key}
		</section>

		<section class="section" aria-labelledby="form-h">
			<div class="section-head">
				<h2 id="form-h">Listing details</h2>
				<p>The address can’t change; list the new one and delete this listing instead.</p>
			</div>
			{#key formKey}
				<NetworkForm mode="edit" initial={n} submitLabel="Save changes" onsubmit={save} />
			{/key}
		</section>
	{/if}

	<section class="section danger" aria-labelledby="del-h">
		<div>
			<h2 id="del-h">Delete this listing</h2>
			<p>
				It disappears from this directory and from every directory that imports it. You can list it again
				later.
			</p>
		</div>
		<Confirm title="Delete {n.name}?" action="Delete listing" danger typed={n.hostname} onconfirm={remove}>
			{#snippet trigger(open)}<button class="btn btn-danger" onclick={open}>Delete listing</button>{/snippet}
			This removes <strong>{n.name}</strong> and its images. Peers drop it at their next sync.
		</Confirm>
	</section>
{/if}

<style>
	.head {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-4) var(--space-5);
	}
	.title {
		flex: 1;
		min-width: min(100%, 240px);
		display: grid;
		gap: var(--space-2);
	}
	h1 {
		overflow-wrap: anywhere;
	}
	.sub {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2) var(--space-3);
	}
	.addr {
		font-family: var(--font-mono);
		font-size: var(--text-sm);
		color: var(--fg-2);
	}
	.acts {
		display: flex;
		gap: var(--space-2);
		flex-wrap: wrap;
	}
	.verified p {
		color: var(--fg-2);
		font-size: var(--text-sm);
		max-width: 72ch;
		padding: var(--space-4) var(--space-5);
		border: 1px solid var(--border);
		border-left: 3px solid var(--success);
		border-radius: 0 var(--radius-sm) var(--radius-sm) 0;
		background: var(--surface);
	}
	.section.verified {
		margin-top: var(--space-8);
	}
	.imgs {
		display: grid;
		grid-template-columns: minmax(0, 1fr) minmax(0, 1.4fr);
		gap: var(--space-8);
		max-width: 860px;
	}
	.danger {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-4);
		padding: var(--space-5);
		border: 1px solid color-mix(in oklab, var(--danger), transparent 70%);
		border-radius: var(--radius-sm);
		max-width: 860px;
	}
	.danger p {
		margin-top: var(--space-1);
		color: var(--fg-2);
		font-size: var(--text-sm);
	}
	@media (max-width: 640px) {
		.imgs {
			grid-template-columns: minmax(0, 1fr);
		}
	}
</style>
