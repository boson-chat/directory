<script lang="ts">
	import { untrack } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { directory, ApiError } from '$lib/api';
	import type { Facets, Network, Sort } from '$lib/types';
	import { readFilters, writeFilters, isFiltered, DEFAULT_FILTERS, type Filters } from '$lib/filters';
	import { langName } from '$lib/links';
	import { num } from '$lib/format';
	import type { DropdownOption } from '$lib/dropdown';
	import Dropdown from '$lib/components/Dropdown.svelte';
	import Toggle from '$lib/components/Toggle.svelte';
	import State from '$lib/components/State.svelte';
	import NetworkCard from '$lib/components/NetworkCard.svelte';

	let f = $state<Filters>(readFilters(page.url.searchParams));
	let query = $state(readFilters(page.url.searchParams).q);

	let networks = $state<Network[]>([]);
	let cursor = $state<string | null>(null);
	let total = $state(0);
	let loading = $state(true);
	let error = $state('');
	let moreError = $state('');
	let facets = $state<Facets | null>(null);
	let sentinel = $state<HTMLElement>();
	let ctrl: AbortController | null = null;

	// Follow back/forward navigation.
	$effect(() => {
		const next = readFilters(page.url.searchParams);
		untrack(() => {
			if (writeFilters(next) !== writeFilters(f)) {
				f = next;
				query = next.q;
			}
		});
	});

	function apply(patch: Partial<Filters>) {
		f = { ...f, ...patch };
		goto(writeFilters(f) || '/', { replaceState: true, keepFocus: true, noScroll: true });
	}

	let debounce: ReturnType<typeof setTimeout>;
	function onSearch() {
		clearTimeout(debounce);
		debounce = setTimeout(() => apply({ q: query }), 250);
	}

	async function load(more = false) {
		ctrl?.abort();
		const c = (ctrl = new AbortController());
		loading = true;
		if (more) moreError = '';
		else error = '';
		try {
			const r = await directory.list({ ...f, limit: 24, cursor: more ? cursor : null }, c.signal);
			if (c.signal.aborted) return;
			networks = more ? [...networks, ...r.networks] : r.networks;
			cursor = r.next_cursor;
			total = r.total_estimate;
		} catch (e) {
			if (e instanceof DOMException && e.name === 'AbortError') return;
			const msg = e instanceof ApiError || e instanceof Error ? e.message : String(e);
			if (more) moreError = msg;
			else error = msg;
		} finally {
			if (ctrl === c) loading = false;
		}
	}

	// Reload from the first page whenever a filter changes.
	$effect(() => {
		void writeFilters(f);
		untrack(() => {
			networks = [];
			cursor = null;
			load();
		});
	});

	$effect(() => {
		const nsfw = f.nsfw;
		directory
			.facets(nsfw)
			.then((x) => (facets = x))
			.catch(() => (facets = { tags: [], languages: [] }));
	});

	// Infinite scroll: fetch the next page as the sentinel nears the viewport.
	$effect(() => {
		if (!sentinel) return;
		const io = new IntersectionObserver(
			(entries) => {
				if (entries.some((e) => e.isIntersecting) && cursor && !loading && !moreError) load(true);
			},
			{ rootMargin: '600px 0px' }
		);
		io.observe(sentinel);
		return () => io.disconnect();
	});

	const plural = (n: number, one: string, many: string) => `${num(n)} ${n === 1 ? one : many}`;

	let langOptions = $derived<DropdownOption[]>([
		{ value: '', label: 'Any language' },
		...(facets?.languages ?? []).map((l) => ({
			value: l.value,
			label: langName(l.value),
			description: plural(l.count, 'network', 'networks')
		}))
	]);
	let tagOptions = $derived<DropdownOption[]>([
		{ value: '', label: 'Any topic' },
		...(facets?.tags ?? []).map((t) => ({
			value: t.value,
			label: t.value,
			description: plural(t.count, 'network', 'networks')
		}))
	]);
	let sourceOptions = $derived<DropdownOption[]>([
		{ value: 'all', label: 'All directories', description: 'This directory and its peers' },
		// Without per-source counts from the API, still offer "listed here".
		...(facets?.sources ?? [{ id: 'local', name: 'This directory', count: -1 }]).map((s) => ({
			value: s.id,
			label: s.id === 'local' ? 'Listed here' : s.name,
			description: [
				s.id === 'local' ? 'Registered with this directory' : (s.url?.replace(/^https?:\/\//, '') ?? ''),
				s.count >= 0 ? plural(s.count, 'network', 'networks') : ''
			]
				.filter(Boolean)
				.join(' · ')
		}))
	]);
	const sortOptions: DropdownOption<Sort>[] = [
		{ value: 'users', label: 'Most users' },
		{ value: 'active', label: 'Recently active' },
		{ value: 'newest', label: 'Newest' },
		{ value: 'name', label: 'Name' }
	];
	let popular = $derived((facets?.tags ?? []).slice(0, 8));
	let sourceCount = $derived((facets?.sources ?? []).filter((s) => s.count > 0).length);
	let countText = $derived(
		plural(total, 'network', 'networks') +
			(sourceCount > 1 && f.source === 'all' ? ` from ${sourceCount} directories` : '')
	);
</script>

<svelte:head><title>Browse networks · boson directory</title></svelte:head>

<section class="hero" aria-labelledby="h">
	<h1 id="h">Find a network to join</h1>
	<p class="lede">
		IRC networks people run for every kind of community. Each one proves it owns its hostname with a DNS
		record before it shows up here.
	</p>
	<form class="search" role="search" onsubmit={(e) => (e.preventDefault(), apply({ q: query }))}>
		<label class="sr-only" for="q">Search networks</label>
		<span class="prompt" aria-hidden="true">/</span>
		<input
			id="q"
			type="search"
			bind:value={query}
			oninput={onSearch}
			placeholder="Search by name, topic or hostname"
			autocomplete="off"
			spellcheck="false"
		/>
		<span class="count" aria-live="polite">
			{#if !loading || networks.length}{countText}{/if}
		</span>
	</form>
	{#if popular.length}
		<div class="chips" aria-label="Popular topics">
			{#each popular as t (t.value)}
				<button
					class="chip"
					type="button"
					aria-pressed={f.tag === t.value}
					onclick={() => apply({ tag: f.tag === t.value ? '' : t.value })}>{t.value}</button
				>
			{/each}
		</div>
	{/if}
</section>

<div class="filters" aria-label="Filters">
	<div class="f">
		<span class="flabel" id="fl-lang">Language</span>
		<Dropdown
			options={langOptions}
			value={f.lang}
			onchange={(v) => apply({ lang: v })}
			labelledby="fl-lang"
			size="small"
		/>
	</div>
	<div class="f">
		<span class="flabel" id="fl-tag">Topic</span>
		<Dropdown
			options={tagOptions}
			value={f.tag}
			onchange={(v) => apply({ tag: v })}
			labelledby="fl-tag"
			size="small"
		/>
	</div>
	<div class="f">
		<span class="flabel" id="fl-src">Source</span>
		<Dropdown
			options={sourceOptions}
			value={f.source}
			onchange={(v) => apply({ source: v })}
			labelledby="fl-src"
			size="small"
		/>
	</div>
	<div class="f">
		<span class="flabel" id="fl-sort">Sort</span>
		<Dropdown
			options={sortOptions}
			value={f.sort}
			onchange={(v) => apply({ sort: v })}
			labelledby="fl-sort"
			size="small"
		/>
	</div>
	<div class="f nsfw">
		<Toggle label="Show 18+" checked={f.nsfw} onchange={(v) => apply({ nsfw: v })} />
	</div>
	{#if isFiltered(f)}
		<button
			class="btn btn-small btn-ghost clear"
			type="button"
			onclick={() => {
				query = '';
				apply({ ...DEFAULT_FILTERS, sort: f.sort });
			}}>Clear filters</button
		>
	{/if}
</div>

{#if error}
	<State kind="error" title="The directory didn’t answer">
		{error}
		{#snippet action()}<button class="btn btn-small" onclick={() => load()}>Try again</button>{/snippet}
	</State>
{:else if loading && !networks.length}
	<ul class="grid" aria-busy="true" aria-label="Loading networks">
		{#each [0, 1, 2, 3, 4, 5] as i (i)}<li class="ghost" aria-hidden="true"></li>{/each}
	</ul>
{:else if !networks.length}
	{#if isFiltered(f)}
		<State kind="empty" title="No networks match">
			Try a shorter search, another language or topic, or look in every directory.
			{#snippet action()}
				<button
					class="btn btn-small"
					onclick={() => {
						query = '';
						apply({ ...DEFAULT_FILTERS, sort: f.sort });
					}}>Clear filters</button
				>
			{/snippet}
		</State>
	{:else}
		<State kind="empty" title="No networks are listed yet">
			Run an IRC network? Be the first: register it and prove you own the hostname with a TXT record.
			{#snippet action()}<a class="btn btn-small btn-primary" href="/me/new">List your network</a>{/snippet}
		</State>
	{/if}
{:else}
	<ul class="grid">
		{#each networks as n (n.id)}
			<li><NetworkCard network={n} /></li>
		{/each}
	</ul>
	<div class="more" bind:this={sentinel}>
		{#if moreError}
			<State kind="error" title="The next page didn’t load">
				{moreError}
				{#snippet action()}<button class="btn btn-small" onclick={() => load(true)}>Try again</button
					>{/snippet}
			</State>
		{:else if cursor}
			<button class="btn" onclick={() => load(true)} disabled={loading}
				>{loading ? 'Loading more…' : 'Show more networks'}</button
			>
		{:else if networks.length > 12}
			<p class="end">That’s every network{isFiltered(f) ? ' that matches' : ''}.</p>
		{/if}
	</div>
{/if}

<section class="cta" aria-labelledby="cta-h">
	<div>
		<h2 id="cta-h">Run a network?</h2>
		<p>List it here with your boson account. Boson stacks can register from their admin panel.</p>
	</div>
	<a class="btn" href="/me/new">List your network</a>
</section>

<style>
	.hero {
		padding: var(--space-6) 0 var(--space-6);
		max-width: 760px;
	}
	h1 {
		font-size: clamp(28px, 4.4vw, 40px);
		letter-spacing: -0.03em;
	}
	.lede {
		margin-top: var(--space-3);
		color: var(--fg-2);
		font-size: var(--text-lg);
		max-width: 60ch;
	}
	.search {
		position: relative;
		margin-top: var(--space-6);
		display: flex;
		align-items: center;
	}
	.prompt {
		position: absolute;
		left: var(--space-4);
		font-family: var(--font-mono);
		font-size: var(--text-xl);
		font-weight: 700;
		color: var(--accent);
		pointer-events: none;
	}
	.search input {
		min-height: 56px;
		padding-left: 44px;
		padding-right: 14ch;
		font-size: var(--text-lg);
		background: var(--surface-raised);
		border-color: var(--border-strong);
	}
	.count {
		position: absolute;
		right: var(--space-4);
		font-family: var(--font-mono);
		font-size: var(--text-xs);
		color: var(--fg-quiet);
		pointer-events: none;
		white-space: nowrap;
	}
	.chips {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
		margin-top: var(--space-4);
	}
	.chip {
		font-family: var(--font-mono);
		font-size: var(--text-xs);
		color: var(--fg-2);
		background: transparent;
		border: 1px solid var(--border);
		border-radius: 999px;
		padding: 3px 10px;
		cursor: pointer;
		transition:
			border-color var(--motion-fast) var(--ease-standard),
			color var(--motion-fast) var(--ease-standard);
	}
	.chip:hover {
		color: var(--fg);
		border-color: var(--fg-3);
	}
	.chip[aria-pressed='true'] {
		color: var(--accent-on);
		background: var(--accent);
		border-color: var(--accent);
	}
	.filters {
		display: flex;
		flex-wrap: wrap;
		align-items: flex-end;
		gap: var(--space-3) var(--space-4);
		padding: var(--space-4) 0;
		margin-bottom: var(--space-6);
		border-top: 1px solid var(--border-soft);
		border-bottom: 1px solid var(--border-soft);
	}
	.f {
		display: grid;
		gap: var(--space-1);
		min-width: 150px;
	}
	.flabel {
		font-family: var(--font-mono);
		font-size: var(--text-xs);
		color: var(--fg-quiet);
	}
	.nsfw {
		min-width: 0;
		padding-bottom: 5px;
	}
	.clear {
		margin-left: auto;
	}
	.grid {
		list-style: none;
		margin: 0;
		padding: 0;
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(min(100%, 280px), 1fr));
		gap: var(--space-4);
	}
	.grid > li {
		display: grid;
	}
	.ghost {
		height: 300px;
		border: 1px solid var(--border-soft);
		border-radius: var(--radius-md);
		background: linear-gradient(90deg, var(--surface) 0%, var(--surface-raised) 50%, var(--surface) 100%);
		background-size: 200% 100%;
		animation: shimmer 1.4s linear infinite;
	}
	@keyframes shimmer {
		to {
			background-position: -200% 0;
		}
	}
	.more {
		display: flex;
		justify-content: center;
		padding: var(--space-8) 0 0;
	}
	.end {
		font-family: var(--font-mono);
		font-size: var(--text-xs);
		color: var(--fg-quiet);
	}
	.cta {
		margin-top: var(--space-16);
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-4);
		padding: var(--space-6);
		border: 1px solid var(--border);
		border-left: 3px solid var(--accent);
		border-radius: 0 var(--radius-sm) var(--radius-sm) 0;
		background: var(--surface);
	}
	.cta p {
		margin-top: var(--space-1);
		color: var(--fg-2);
		font-size: var(--text-sm);
	}
	@media (max-width: 640px) {
		.f {
			min-width: 0;
			flex: 1 1 calc(50% - var(--space-4));
		}
		.search input {
			padding-right: var(--space-3);
		}
		.count {
			position: static;
		}
		.search {
			flex-direction: column;
			align-items: stretch;
			gap: var(--space-2);
		}
		.prompt {
			top: 14px;
		}
		.clear {
			margin-left: 0;
		}
	}
</style>
