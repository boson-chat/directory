<script lang="ts">
	import type { Snippet } from 'svelte';
	import { page } from '$app/state';
	import SignedIn from '$lib/components/SignedIn.svelte';
	let { children }: { children: Snippet } = $props();

	const tabs = [
		{ href: '/operator/peers', label: 'Peers' },
		{ href: '/operator/listings', label: 'Listings' },
		{ href: '/operator/blocks', label: 'Blocks' },
		{ href: '/operator/audit', label: 'Audit log' }
	];
</script>

<SignedIn perm="directory.manage">
	<div class="op">
		<div class="top">
			<h1>Operator</h1>
			<p>Run this directory: where it imports listings from, what it hides, and who changed what.</p>
		</div>
		<nav aria-label="Operator">
			<ul>
				{#each tabs as t (t.href)}
					<li>
						<a href={t.href} aria-current={page.url.pathname.startsWith(t.href) ? 'page' : undefined}
							>{t.label}</a
						>
					</li>
				{/each}
			</ul>
		</nav>
		<div class="body">{@render children()}</div>
	</div>
</SignedIn>

<style>
	.top p {
		color: var(--fg-2);
		margin-top: var(--space-2);
		max-width: 64ch;
	}
	nav {
		margin: var(--space-6) 0 var(--space-8);
		border-bottom: 1px solid var(--border);
		overflow-x: auto;
	}
	ul {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		gap: var(--space-1);
	}
	a {
		display: block;
		padding: var(--space-2) var(--space-3) var(--space-3);
		color: var(--fg-2);
		text-decoration: none;
		font-family: var(--font-display);
		font-size: var(--text-sm);
		font-weight: 600;
		white-space: nowrap;
		border-bottom: 2px solid transparent;
		margin-bottom: -1px;
	}
	a:hover {
		color: var(--fg);
	}
	a[aria-current='page'] {
		color: var(--accent-text);
		border-bottom-color: var(--accent);
	}
</style>
