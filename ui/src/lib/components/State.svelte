<script lang="ts">
	import type { Snippet } from 'svelte';

	/**
	 * One look for every non-content state of a page or section:
	 * loading, empty (an invitation to act), error (what failed and what to
	 * do), unsupported (an upstream release is too old) and off (a feature
	 * that isn't configured).
	 */
	let {
		kind,
		title,
		children,
		action
	}: {
		kind: 'loading' | 'empty' | 'error' | 'unsupported' | 'off';
		title?: string;
		children?: Snippet;
		action?: Snippet;
	} = $props();

	const defaults: Record<typeof kind, string> = {
		loading: 'Loading…',
		empty: 'Nothing here yet',
		error: 'This didn’t load',
		unsupported: 'Needs a newer release',
		off: 'Not set up'
	};
</script>

<div
	class="state {kind}"
	role={kind === 'error' ? 'alert' : kind === 'loading' ? 'status' : undefined}
	aria-busy={kind === 'loading' || undefined}
>
	<span class="glyph" aria-hidden="true"></span>
	<div class="text">
		<p class="title">{title ?? defaults[kind]}</p>
		{#if children}<div class="body">{@render children()}</div>{/if}
		{#if action}<div class="act">{@render action()}</div>{/if}
	</div>
</div>

<style>
	.state {
		display: flex;
		gap: var(--space-4);
		align-items: flex-start;
		padding: var(--space-6) var(--space-5);
		border: 1px dashed var(--border-strong);
		border-radius: var(--radius-sm);
		background: var(--surface);
		max-width: 72ch;
	}
	.loading {
		border-style: solid;
		border-color: var(--border-soft);
		background: transparent;
		padding: var(--space-4) var(--space-5);
	}
	.error {
		border-style: solid;
		border-color: color-mix(in oklab, var(--danger), transparent 70%);
		border-left: 3px solid var(--danger);
	}
	.unsupported {
		border-style: solid;
		border-left: 3px solid var(--info);
	}
	.glyph {
		flex: none;
		width: 10px;
		height: 10px;
		margin-top: 5px;
		border-radius: 50%;
		border: 2px solid var(--fg-3);
	}
	.loading .glyph {
		border-color: var(--accent);
		border-right-color: transparent;
		animation: spin 0.9s linear infinite;
	}
	.error .glyph {
		border-color: var(--danger);
		background: var(--danger);
	}
	.unsupported .glyph {
		border-color: var(--info);
	}
	.empty .glyph {
		border-style: dashed;
	}
	.text {
		display: grid;
		gap: var(--space-2);
		min-width: 0;
	}
	.title {
		font-family: var(--font-display);
		font-weight: 600;
		color: var(--fg);
		letter-spacing: var(--tracking-display);
	}
	.loading .title {
		color: var(--fg-quiet);
		font-weight: 500;
		font-size: var(--text-sm);
	}
	.error .title {
		color: var(--danger-text);
	}
	.body {
		color: var(--fg-2);
		font-size: var(--text-sm);
		line-height: 1.6;
		overflow-wrap: anywhere;
	}
	.body :global(p + p) {
		margin-top: var(--space-2);
	}
	.act {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
		margin-top: var(--space-1);
	}
	@keyframes spin {
		to {
			transform: rotate(360deg);
		}
	}
</style>
