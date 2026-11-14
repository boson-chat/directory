<script lang="ts">
	import type { Network } from '$lib/types';
	import { address, joinUrl, langName, originLabel } from '$lib/links';
	import NetworkIcon from './NetworkIcon.svelte';
	import Health from './Health.svelte';

	let { network: n }: { network: Network } = $props();
	let bannerFailed = $state(false);
</script>

<article class="card" aria-labelledby="nc-{n.id}">
	<div class="banner" class:empty={!n.banner_url || bannerFailed}>
		{#if n.banner_url && !bannerFailed}
			<img src={n.banner_url} alt="" loading="lazy" onerror={() => (bannerFailed = true)} />
		{/if}
	</div>
	<div class="body">
		<div class="head">
			<span class="ic"><NetworkIcon name={n.name} url={n.icon_url} /></span>
			<div class="title">
				<h2 id="nc-{n.id}"><a href="/n/{encodeURIComponent(n.id)}">{n.name}</a></h2>
				<span class="addr">{address(n)}</span>
			</div>
		</div>
		{#if n.description}<p class="desc">{n.description}</p>{/if}
		<div class="meta">
			<Health status={n.health_status} users={n.user_count} />
			{#if n.languages.length}
				<span class="langs" title={n.languages.map(langName).join(', ')}
					>{n.languages.slice(0, 3).join(' ').toUpperCase()}{n.languages.length > 3 ? ' +' : ''}</span
				>
			{/if}
		</div>
		{#if n.tags.length || n.is_nsfw || n.origin !== 'local'}
			<div class="tags">
				{#if n.is_nsfw}<span class="tag tag-bad">18+</span>{/if}
				{#each n.tags.slice(0, 4) as t (t)}<span class="tag">{t}</span>{/each}
				{#if n.origin !== 'local'}
					<span class="tag tag-src" title="Imported from {originLabel(n)}, re-checked here"
						>via {originLabel(n)}</span
					>
				{/if}
			</div>
		{/if}
		<div class="actions">
			<a class="btn btn-small join" href={joinUrl(n)}>Open in Boson</a>
		</div>
	</div>
</article>

<style>
	.card {
		position: relative;
		display: flex;
		flex-direction: column;
		background: var(--surface);
		border: 1px solid var(--border);
		border-radius: var(--radius-md);
		overflow: hidden;
		transition: border-color var(--motion-fast) var(--ease-standard);
	}
	.card:hover {
		border-color: var(--border-strong);
	}
	.card:focus-within {
		border-color: var(--accent);
	}
	.banner {
		aspect-ratio: 3 / 1;
		background: var(--surface-raised);
		border-bottom: 1px solid var(--border-soft);
	}
	.banner.empty {
		background:
			repeating-linear-gradient(
				135deg,
				transparent 0 11px,
				color-mix(in oklab, var(--border), transparent 40%) 11px 12px
			),
			var(--surface-raised);
	}
	.banner img {
		width: 100%;
		height: 100%;
		object-fit: cover;
		display: block;
	}
	.body {
		display: flex;
		flex-direction: column;
		gap: var(--space-3);
		padding: 0 var(--space-4) var(--space-4);
		flex: 1;
	}
	.head {
		display: flex;
		align-items: flex-end;
		gap: var(--space-3);
	}
	.ic {
		margin-top: -24px;
		border-radius: var(--radius-md);
		box-shadow: 0 0 0 3px var(--surface);
		display: inline-flex;
	}
	.title {
		min-width: 0;
		display: grid;
		gap: 2px;
		padding-bottom: 1px;
	}
	h2 {
		font-size: var(--text-base);
		font-weight: 700;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	h2 a {
		color: var(--fg);
		text-decoration: none;
	}
	/* the whole card opens the detail page; the join button sits above it */
	h2 a::after {
		content: '';
		position: absolute;
		inset: 0;
	}
	h2 a:focus-visible {
		box-shadow: none;
	}
	.addr {
		font-family: var(--font-mono);
		font-size: var(--text-xs);
		color: var(--fg-quiet);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.desc {
		color: var(--fg-2);
		font-size: var(--text-sm);
		line-height: 1.5;
		display: -webkit-box;
		-webkit-line-clamp: 2;
		line-clamp: 2;
		-webkit-box-orient: vertical;
		overflow: hidden;
	}
	.meta {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-3);
		margin-top: auto;
	}
	.langs {
		font-family: var(--font-mono);
		font-size: var(--text-xs);
		color: var(--fg-quiet);
		letter-spacing: var(--tracking-label);
	}
	.tags {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-1);
	}
	.tag {
		text-transform: none;
		letter-spacing: 0.01em;
	}
	.tag-src {
		color: var(--info-text);
		background: color-mix(in oklab, var(--info), transparent 90%);
		box-shadow: none;
	}
	.actions {
		position: relative;
		z-index: 1;
		display: flex;
		padding-top: var(--space-3);
		border-top: 1px solid var(--border-soft);
	}
	.actions .btn {
		width: 100%;
	}
	.join:hover:not(:disabled) {
		color: var(--accent-on);
		background: var(--accent);
		border-color: var(--accent);
	}
</style>
