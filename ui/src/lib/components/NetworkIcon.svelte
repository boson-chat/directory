<script lang="ts">
	/** A network's square icon, or its initial on a tinted tile. */
	let { name, url, size = 48 }: { name: string; url?: string | null; size?: number } = $props();
	let failed = $state(false);
	let initial = $derived((name.trim()[0] ?? '#').toUpperCase());
</script>

<span class="icon" class:s={size <= 32} class:l={size >= 64} aria-hidden="true">
	{#if url && !failed}
		<img src={url} alt="" loading="lazy" onerror={() => (failed = true)} />
	{:else}
		<span class="letter">{initial}</span>
	{/if}
</span>

<style>
	.icon {
		--size: 48px;
		width: var(--size);
		height: var(--size);
		flex: none;
		display: inline-flex;
		align-items: center;
		justify-content: center;
		border-radius: var(--radius-md);
		overflow: hidden;
		background: var(--surface-raised);
		box-shadow: inset 0 0 0 1px var(--border);
	}
	.s {
		--size: 32px;
	}
	.l {
		--size: 72px;
	}
	img {
		width: 100%;
		height: 100%;
		object-fit: cover;
		display: block;
	}
	.letter {
		font-family: var(--font-display);
		font-weight: 700;
		font-size: calc(var(--size) * 0.44);
		color: var(--accent-text);
	}
</style>
