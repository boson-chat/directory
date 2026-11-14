<script lang="ts">
	import { datetime, relative } from '$lib/format';
	let { at, empty = '—' }: { at: string | null | undefined; empty?: string } = $props();
</script>

{#if at}
	<time datetime={at} title={datetime(at)}>{relative(at)}</time>
{:else}
	<span class="muted">{empty}</span>
{/if}
