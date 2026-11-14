<script lang="ts">
	import type { Snippet } from 'svelte';
	let { tone = 'bad', children }: { tone?: 'bad' | 'warn' | 'info'; children: Snippet } = $props();
</script>

<div class="notice {tone}" role={tone === 'bad' ? 'alert' : 'note'}>
	{@render children()}
</div>

<style>
	/* boson WarningBanner: a 3px rule on the leading edge over a faint tint */
	.notice {
		padding: var(--space-3) var(--space-4);
		border: 1px solid;
		border-left-width: 3px;
		border-radius: 0 var(--radius-sm) var(--radius-sm) 0;
		font-size: var(--text-sm);
		line-height: 1.5;
		color: var(--fg-2);
	}
	.bad {
		color: var(--danger-text);
		background: color-mix(in oklab, var(--danger), transparent 92%);
		border-color: color-mix(in oklab, var(--danger), transparent 80%);
		border-left-color: var(--danger);
	}
	.warn {
		background: color-mix(in oklab, var(--warn), transparent 92%);
		border-color: color-mix(in oklab, var(--warn), transparent 80%);
		border-left-color: var(--warn);
	}
	.info {
		background: color-mix(in oklab, var(--info), transparent 92%);
		border-color: color-mix(in oklab, var(--info), transparent 80%);
		border-left-color: var(--info);
	}
</style>
