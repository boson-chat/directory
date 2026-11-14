<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { auth, authErrorText } from '$lib/auth.svelte';
	import State from '$lib/components/State.svelte';

	let error = $state('');
	let started = false;

	$effect(() => {
		if (started) return;
		started = true;
		auth
			.finishSso(page.url.searchParams)
			.then((next) => goto(next, { replaceState: true }))
			.catch((e) => (error = authErrorText(e)));
	});
</script>

<svelte:head><title>Signing in · boson directory</title></svelte:head>

<div class="wrap">
	{#if error}
		<State kind="error" title="Sign-in didn’t finish">
			{error}
			{#snippet action()}<a class="btn btn-small" href="/signin">Back to sign in</a>{/snippet}
		</State>
	{:else}
		<State kind="loading" title="Finishing sign-in…" />
	{/if}
</div>

<style>
	.wrap {
		display: grid;
		place-items: center;
		padding: var(--space-12) 0;
	}
</style>
