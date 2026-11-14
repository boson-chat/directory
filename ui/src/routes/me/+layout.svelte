<script lang="ts">
	import type { Snippet } from 'svelte';
	import SignedIn from '$lib/components/SignedIn.svelte';
	let { children }: { children: Snippet } = $props();
</script>

<SignedIn>{@render children()}</SignedIn>
