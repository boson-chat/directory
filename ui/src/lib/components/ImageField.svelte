<script lang="ts">
	import { directory } from '$lib/api';
	import type { Listing } from '$lib/types';
	import { toasts, errorText } from '$lib/toasts.svelte';
	import NetworkIcon from './NetworkIcon.svelte';

	/** Upload or remove a listing's icon (square) or banner (3:1). */
	let { listing = $bindable(), which }: { listing: Listing; which: 'icon' | 'banner' } = $props();
	let network = $derived(listing.network);
	const uid = $props.id();
	const MAX = 5 * 1024 * 1024;
	const TYPES = ['image/png', 'image/jpeg', 'image/webp', 'image/gif'];
	let busy = $state(false);
	let error = $state('');
	let fileInput = $state<HTMLInputElement>();
	let url = $derived(which === 'icon' ? network.icon_url : network.banner_url);
	let label = $derived(which === 'icon' ? 'Icon' : 'Banner');
	let hint = $derived(
		which === 'icon'
			? 'Square, at least 256 × 256. PNG, JPEG, WebP or GIF up to 5 MB.'
			: 'Wide, 1200 × 400 works best. Up to 5 MB.'
	);

	async function picked(e: Event) {
		const f = (e.currentTarget as HTMLInputElement).files?.[0];
		(e.currentTarget as HTMLInputElement).value = '';
		if (!f) return;
		error = '';
		if (!TYPES.includes(f.type)) return void (error = 'Use a PNG, JPEG, WebP or GIF image.');
		if (f.size > MAX)
			return void (error = `That file is ${(f.size / 1048576).toFixed(1)} MB; the limit is 5 MB.`);
		busy = true;
		try {
			listing = await directory.putImage(network.id, which, f);
			toasts.push(`${label} updated.`);
		} catch (err) {
			error = errorText(err);
		} finally {
			busy = false;
		}
	}
	async function remove() {
		busy = true;
		error = '';
		try {
			listing = await directory.deleteImage(network.id, which);
			toasts.push(`${label} removed.`);
		} catch (err) {
			error = errorText(err);
		} finally {
			busy = false;
		}
	}
</script>

<div class="img {which}">
	<div class="preview" aria-hidden="true">
		{#if which === 'icon'}
			<NetworkIcon name={network.name} {url} size={72} />
		{:else if url}
			<img src={url} alt="" />
		{:else}
			<span class="none">No banner</span>
		{/if}
	</div>
	<div class="ctl">
		<span class="lbl" id="{uid}-l">{label}</span>
		<small id="{uid}-h">{hint}</small>
		<div class="btns">
			<input
				bind:this={fileInput}
				class="sr-only"
				type="file"
				accept={TYPES.join(',')}
				onchange={picked}
				tabindex="-1"
				aria-hidden="true"
			/>
			<button
				class="btn btn-small"
				type="button"
				disabled={busy}
				aria-describedby="{uid}-h"
				onclick={() => fileInput?.click()}
				>{busy ? 'Uploading…' : url ? `Replace ${which}` : `Upload ${which}`}</button
			>
			{#if url}<button class="btn btn-small btn-ghost" type="button" disabled={busy} onclick={remove}
					>Remove</button
				>{/if}
		</div>
		{#if error}<small class="err" role="alert">{error}</small>{/if}
	</div>
</div>

<style>
	.img {
		display: flex;
		gap: var(--space-4);
		align-items: flex-start;
		min-width: 0;
	}
	.banner {
		flex-direction: column;
	}
	.preview {
		flex: none;
	}
	.banner .preview {
		width: 100%;
		aspect-ratio: 3 / 1;
		border-radius: var(--radius-md);
		overflow: hidden;
		background:
			repeating-linear-gradient(
				135deg,
				transparent 0 11px,
				color-mix(in oklab, var(--border), transparent 40%) 11px 12px
			),
			var(--surface-raised);
		box-shadow: inset 0 0 0 1px var(--border);
		display: grid;
		place-items: center;
	}
	.banner img {
		width: 100%;
		height: 100%;
		object-fit: cover;
		display: block;
	}
	.none {
		font-family: var(--font-mono);
		font-size: var(--text-xs);
		color: var(--fg-quiet);
	}
	.ctl {
		display: grid;
		gap: var(--space-1);
		min-width: 0;
	}
	.lbl {
		font-family: var(--font-display);
		font-weight: 600;
		font-size: var(--text-sm);
	}
	small {
		font-family: var(--font-mono);
		font-size: var(--text-xs);
		color: var(--fg-quiet);
	}
	.err {
		color: var(--danger-text);
	}
	.btns {
		display: flex;
		gap: var(--space-2);
		margin-top: var(--space-2);
	}
</style>
