<script lang="ts">
	import { page } from '$app/state';
	import State from '$lib/components/State.svelte';
</script>

<svelte:head><title>Not found · boson directory</title></svelte:head>

{#if page.status === 404}
	<State kind="empty" title="There’s no page here">
		The address may be mistyped, or the page moved.
		{#snippet action()}<a class="btn btn-small" href="/">Browse networks</a>{/snippet}
	</State>
{:else}
	<State kind="error" title="This page didn’t load">
		{page.error?.message ?? 'Something went wrong.'}
		{#snippet action()}<a class="btn btn-small" href="/">Browse networks</a>{/snippet}
	</State>
{/if}
