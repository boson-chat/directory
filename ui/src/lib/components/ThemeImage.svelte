<script lang="ts">
	import { directory } from '$lib/api';
	import type { Listing } from '$lib/types';
	import { errorText, toasts } from '$lib/toasts.svelte';
	import { readTheme, slotValue, type ImageSlot } from '$lib/theme';
	import { FileDrop } from '$lib/drop.svelte';

	/** Upload or remove one theme image slot. Uploads go straight to the server. */
	let {
		listing = $bindable(),
		slot,
		label,
		hint,
		onchange
	}: {
		listing: Listing;
		slot: ImageSlot;
		label: string;
		hint: string;
		/** Called with the server's new value (undefined when removed). */
		onchange?: (url: string | undefined) => void;
	} = $props();

	const uid = $props.id();
	const MAX = 2 * 1024 * 1024;
	const TYPES = ['image/png', 'image/jpeg', 'image/webp', 'image/gif', 'image/svg+xml'];
	let busy = $state(false);
	let error = $state('');
	let input = $state<HTMLInputElement>();
	let url = $derived(slotValue(readTheme(listing.network.theme), slot));

	function picked(e: Event) {
		const el = e.currentTarget as HTMLInputElement;
		const f = el.files?.[0];
		el.value = '';
		if (f) void upload(f);
	}
	const drop = new FileDrop(
		(f) => void upload(f),
		() => busy
	);

	async function upload(f: File) {
		if (busy) return;
		error = '';
		if (!TYPES.includes(f.type)) return void (error = "That's not a PNG, JPEG, WebP, GIF or SVG image.");
		if (f.size > MAX)
			return void (error = `That file is ${(f.size / 1048576).toFixed(1)} MB; the limit is 2 MB.`);
		busy = true;
		try {
			listing = await directory.putThemeImage(listing.network.id, slot, f);
			onchange?.(slotValue(readTheme(listing.network.theme), slot));
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
			listing = await directory.deleteThemeImage(listing.network.id, slot);
			onchange?.(undefined);
			toasts.push(`${label} removed.`);
		} catch (err) {
			error = errorText(err);
		} finally {
			busy = false;
		}
	}
</script>

<div class="ti" class:over={drop.over} data-drop={slot} {@attach drop.attach}>
	<span class="thumb" class:empty={!url} aria-hidden="true">
		{#if url}<img src={url} alt="" />{/if}
	</span>
	<div class="ctl">
		<span class="lbl">{label}</span>
		<small id="{uid}-h">{hint}</small>
		<div class="btns">
			<input
				bind:this={input}
				class="sr-only"
				type="file"
				accept={TYPES.join(',')}
				onchange={picked}
				tabindex="-1"
				aria-hidden="true"
				data-slot={slot}
			/>
			<button
				class="btn btn-small"
				type="button"
				disabled={busy}
				aria-describedby="{uid}-h"
				aria-label="{url ? 'Replace' : 'Upload'} {label.toLowerCase()}"
				onclick={() => input?.click()}>{busy ? 'Uploading…' : url ? 'Replace' : 'Upload'}</button
			>
			{#if url}<button
					class="btn btn-small btn-ghost"
					type="button"
					disabled={busy}
					aria-label="Remove {label.toLowerCase()}"
					onclick={remove}>Remove</button
				>{/if}
			<small class="or" class:on={drop.over}>{drop.over ? 'Drop to upload' : 'or drop an image here'}</small>
		</div>
		{#if error}<small class="err" role="alert">{error}</small>{/if}
	</div>
</div>

<style>
	.ti {
		display: flex;
		gap: var(--space-3);
		align-items: flex-start;
		min-width: 0;
		border-radius: var(--radius-md);
	}
	.ti.over {
		background: var(--accent-dim);
		box-shadow: 0 0 0 var(--space-1) var(--accent-dim);
		outline: 1px dashed var(--accent);
		outline-offset: var(--space-1);
	}
	.thumb {
		width: 56px;
		height: 56px;
		flex: none;
		border-radius: var(--radius-md);
		overflow: hidden;
		background: var(--surface-raised);
		box-shadow: inset 0 0 0 1px var(--border);
	}
	.thumb.empty {
		background:
			repeating-linear-gradient(
				135deg,
				transparent 0 7px,
				color-mix(in oklab, var(--border), transparent 40%) 7px 8px
			),
			var(--surface-raised);
	}
	.thumb img {
		width: 100%;
		height: 100%;
		object-fit: cover;
		display: block;
	}
	.ctl {
		display: grid;
		gap: 2px;
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
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-2);
		margin-top: var(--space-1);
	}
	.or.on {
		color: var(--accent-text);
		font-weight: 600;
	}
</style>
