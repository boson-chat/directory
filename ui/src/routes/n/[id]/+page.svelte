<script lang="ts">
	import { page } from '$app/state';
	import { auth } from '$lib/auth.svelte';
	import { directory, ApiError } from '$lib/api';
	import type { Network } from '$lib/types';
	import { address, joinUrl, langName, originLabel } from '$lib/links';
	import { date } from '$lib/format';
	import NetworkIcon from '$lib/components/NetworkIcon.svelte';
	import Health from '$lib/components/Health.svelte';
	import StatusTag from '$lib/components/StatusTag.svelte';
	import CopyField from '$lib/components/CopyField.svelte';
	import State from '$lib/components/State.svelte';
	import Time from '$lib/components/Time.svelte';
	import { listingAccent, listingStyle, readTheme } from '$lib/theme';
	import { scheme } from '$lib/scheme.svelte';

	let n = $state<Network | null>(null);
	let owned = $state(false);
	let error = $state<{ status: number; message: string } | null>(null);
	let bannerFailed = $state(false);
	let theme = $derived(readTheme(n?.theme));
	let style = $derived(listingStyle(theme, scheme.current));
	let accent = $derived(listingAccent(theme, scheme.current));

	async function load(id: string) {
		error = null;
		n = null;
		try {
			const l = await directory.get(id);
			n = l.network;
			owned = !!l.verification || !!l.network.owner;
		} catch (e) {
			error =
				e instanceof ApiError ? { status: e.status, message: e.message } : { status: 0, message: String(e) };
		}
	}

	// Wait for the stored session so owners see their own pending listing.
	$effect(() => {
		const id = page.params.id ?? '';
		if (auth.ready) load(id);
	});
</script>

<svelte:head><title>{n ? n.name : 'Network'} · boson directory</title></svelte:head>

<p class="back"><a href="/">Browse networks</a></p>

{#if error}
	{#if error.status === 404}
		<State kind="empty" title="This network isn’t listed">
			It may have been removed, or it hasn’t passed its DNS check yet.
			{#snippet action()}<a class="btn btn-small" href="/">Browse networks</a>{/snippet}
		</State>
	{:else}
		<State kind="error">
			{error.message}
			{#snippet action()}<button class="btn btn-small" onclick={() => load(page.params.id ?? '')}
					>Try again</button
				>{/snippet}
		</State>
	{/if}
{:else if !n}
	<State kind="loading" title="Loading the network…" />
{:else}
	<article class="net" class:themed={!!style} {style}>
		<div class="banner" class:empty={!n.banner_url || bannerFailed} class:tinted={!!accent}>
			{#if n.banner_url && !bannerFailed}
				<img src={n.banner_url} alt="" onerror={() => (bannerFailed = true)} />
			{/if}
		</div>
		<header class="head">
			<span class="ic"><NetworkIcon name={n.name} url={n.icon_url ?? theme?.logo} size={72} /></span>
			<div class="title">
				<h1>{n.name}</h1>
				<div class="sub">
					<span class="addr">{address(n)}</span>
					{#if n.status !== 'verified'}<StatusTag status={n.status} />{/if}
					{#if n.is_nsfw}<span class="tag tag-bad">18+</span>{/if}
				</div>
			</div>
			<div class="acts">
				{#if owned}<a class="btn" href="/me/{encodeURIComponent(n.id)}">Manage listing</a>{/if}
				<a class="btn btn-primary" href={joinUrl(n)}>Open in Boson</a>
			</div>
		</header>

		<div class="cols">
			<div class="main">
				{#if n.description}
					<p class="desc">{n.description}</p>
				{:else}
					<p class="desc muted">The operators haven’t written a description yet.</p>
				{/if}
				{#if n.tags.length}
					<div class="tags" aria-label="Topics">
						{#each n.tags as t (t)}<a class="tag" href="/?tag={encodeURIComponent(t)}">{t}</a>{/each}
					</div>
				{/if}
				<div class="connect">
					<h2>Connect from another client</h2>
					<div class="cf">
						<CopyField label="Server" value={n.hostname} />
						<CopyField label="Port" value="{n.port}{n.tls ? ' (TLS)' : ''}" />
					</div>
					<p class="muted small">
						Don’t have boson? “Open in Boson” needs the app installed; any IRC client works with the address
						above.
					</p>
				</div>
			</div>

			<dl class="facts">
				<div>
					<dt>Status</dt>
					<dd><Health status={n.health_status} users={n.user_count} /></dd>
				</div>
				{#if n.health_checked_at}
					<div>
						<dt>Last reachable check</dt>
						<dd><Time at={n.health_checked_at} /></dd>
					</div>
				{/if}
				<div>
					<dt>Languages</dt>
					<dd>{n.languages.length ? n.languages.map(langName).join(', ') : 'Not set'}</dd>
				</div>
				<div>
					<dt>Connection</dt>
					<dd>{n.tls ? 'TLS (encrypted)' : 'Plain text, no TLS'}</dd>
				</div>
				{#if n.website_url}
					<div>
						<dt>Website</dt>
						<dd>
							<a href={n.website_url} rel="noopener noreferrer nofollow" target="_blank"
								>{n.website_url.replace(/^https?:\/\//, '').replace(/\/$/, '')}</a
							>
						</dd>
					</div>
				{/if}
				<div>
					<dt>DNS record</dt>
					<dd>
						{#if n.last_verified_at}Seen <Time at={n.last_verified_at} />{:else}Not verified yet{/if}
					</dd>
				</div>
				<div>
					<dt>Listed</dt>
					<dd>{date(n.created_at)}</dd>
				</div>
				<div>
					<dt>Source</dt>
					<dd>
						{#if n.origin === 'local'}Registered here{:else}Imported from {originLabel(n)}, checked here{/if}
					</dd>
				</div>
			</dl>
		</div>
	</article>
{/if}

<style>
	.net {
		border: 1px solid var(--border);
		border-radius: var(--radius-md);
		background: var(--surface);
		overflow: hidden;
	}
	.banner {
		aspect-ratio: 4 / 1;
		max-height: 260px;
		width: 100%;
		background: var(--surface-raised);
		border-bottom: 1px solid var(--border-soft);
	}
	.banner.empty {
		aspect-ratio: auto;
		height: 96px;
		background:
			repeating-linear-gradient(
				135deg,
				transparent 0 11px,
				color-mix(in oklab, var(--border), transparent 40%) 11px 12px
			),
			var(--surface-raised);
	}
	.banner.empty.tinted {
		background:
			linear-gradient(120deg, color-mix(in oklab, var(--accent), transparent 50%), transparent 70%),
			repeating-linear-gradient(
				135deg,
				transparent 0 11px,
				color-mix(in oklab, var(--accent), transparent 85%) 11px 12px
			),
			var(--surface-raised);
	}
	.themed {
		color: var(--fg);
	}
	.banner img {
		width: 100%;
		height: 100%;
		object-fit: cover;
		display: block;
	}
	.head {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-end;
		gap: var(--space-4) var(--space-5);
		padding: 0 var(--space-6) var(--space-6);
		border-bottom: 1px solid var(--border-soft);
	}
	.ic {
		margin-top: -36px;
		display: inline-flex;
		border-radius: var(--radius-md);
		box-shadow: 0 0 0 4px var(--surface);
	}
	.title {
		flex: 1;
		min-width: min(100%, 240px);
		display: grid;
		gap: var(--space-2);
	}
	h1 {
		font-size: clamp(24px, 3.4vw, 32px);
		letter-spacing: -0.02em;
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
		overflow-wrap: anywhere;
	}
	.acts {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
	}
	.cols {
		display: grid;
		grid-template-columns: minmax(0, 1fr) 320px;
	}
	.main {
		padding: var(--space-6);
		display: grid;
		gap: var(--space-6);
		align-content: start;
	}
	.desc {
		font-size: var(--text-lg);
		line-height: 1.6;
		max-width: 64ch;
		white-space: pre-line;
	}
	.tags {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
	}
	.tags a {
		text-decoration: none;
		text-transform: none;
		letter-spacing: 0.01em;
		font-size: var(--text-xs);
		padding: 3px 8px;
	}
	.tags a:hover {
		color: var(--fg);
		box-shadow: inset 0 0 0 1px var(--fg-3);
	}
	.connect {
		display: grid;
		gap: var(--space-3);
		padding-top: var(--space-6);
		border-top: 1px solid var(--border-soft);
	}
	.cf {
		display: grid;
		grid-template-columns: minmax(0, 2fr) minmax(0, 1fr);
		gap: var(--space-3);
	}
	.small {
		font-size: var(--text-xs);
	}
	.facts {
		margin: 0;
		padding: var(--space-6);
		border-left: 1px solid var(--border-soft);
		display: grid;
		gap: var(--space-4);
		align-content: start;
	}
	.facts div {
		display: grid;
		gap: 2px;
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
	@media (max-width: 820px) {
		.cols {
			grid-template-columns: minmax(0, 1fr);
		}
		.facts {
			border-left: 0;
			border-top: 1px solid var(--border-soft);
			grid-template-columns: repeat(2, minmax(0, 1fr));
		}
		.head {
			padding: 0 var(--space-4) var(--space-5);
		}
		.main,
		.facts {
			padding: var(--space-5) var(--space-4);
		}
		.acts {
			width: 100%;
		}
		.acts .btn {
			flex: 1;
		}
	}
	@media (max-width: 480px) {
		.cf {
			grid-template-columns: minmax(0, 1fr);
		}
	}
</style>
