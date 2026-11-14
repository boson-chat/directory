<script lang="ts">
	import type { Snippet } from 'svelte';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { auth } from '$lib/auth.svelte';
	import State from './State.svelte';

	/** Renders children for signed-in accounts (with `perm`, when given); sends everyone else to sign in. */
	let { perm, children }: { perm?: string; children: Snippet } = $props();

	$effect(() => {
		if (auth.ready && !auth.signedIn) {
			goto('/signin?next=' + encodeURIComponent(page.url.pathname + page.url.search), { replaceState: true });
		}
	});
</script>

{#if !auth.ready || !auth.signedIn}
	<State kind="loading" title="Checking your sign-in…" />
{:else if perm && !auth.can(perm)}
	<State kind="off" title="Your account can’t open this page">
		It needs the <code>{perm}</code> permission on this directory’s boson account service.
		{#snippet action()}<a class="btn btn-small" href="/">Browse networks</a>{/snippet}
	</State>
{:else}
	{@render children()}
{/if}
