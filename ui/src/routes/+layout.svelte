<script lang="ts">
	import '@fontsource-variable/inter';
	import '@fontsource-variable/jetbrains-mono';
	import '../app.css';
	import type { Snippet } from 'svelte';
	import { auth } from '$lib/auth.svelte';
	import Shell from '$lib/components/Shell.svelte';
	import Toaster from '$lib/components/Toaster.svelte';

	let { children }: { children: Snippet } = $props();

	$effect(() => {
		void auth.boot();
	});
</script>

<Shell>
	{@render children()}
</Shell>
<Toaster />
