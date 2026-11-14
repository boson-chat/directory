<script lang="ts">
	import '@fontsource-variable/inter';
	import '@fontsource-variable/jetbrains-mono';
	import '../app.css';
	import type { Snippet } from 'svelte';
	import { auth } from '$lib/auth.svelte';
	import Shell from '$lib/components/Shell.svelte';
	import Toaster from '$lib/components/Toaster.svelte';
	import { guardPageDrops } from '$lib/drop.svelte';

	let { children }: { children: Snippet } = $props();

	$effect(() => {
		void auth.boot();
	});
</script>

<!-- A file dropped outside an image field must not navigate away. -->
<svelte:window ondragover={guardPageDrops} ondrop={guardPageDrops} />

<Shell>
	{@render children()}
</Shell>
<Toaster />
