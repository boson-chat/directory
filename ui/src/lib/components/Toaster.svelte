<script lang="ts">
	import { toasts } from '$lib/toasts.svelte';
</script>

<div class="toaster" role="status" aria-live="polite">
	{#each toasts.list as t (t.id)}
		<div class="toast" class:bad={t.tone === 'bad'}>
			<span>{t.text}</span>
			<button class="x" onclick={() => toasts.dismiss(t.id)} aria-label="Dismiss">×</button>
		</div>
	{/each}
</div>

<style>
	.toaster {
		position: fixed;
		right: var(--space-4);
		bottom: var(--space-4);
		display: grid;
		gap: var(--space-2);
		z-index: 50;
		max-width: min(420px, calc(100vw - 32px));
	}
	.toast {
		display: flex;
		align-items: flex-start;
		gap: var(--space-3);
		padding: var(--space-3) var(--space-4);
		background: var(--surface-raised);
		border: 1px solid var(--border);
		border-left: 3px solid var(--success);
		border-radius: 0 var(--radius-sm) var(--radius-sm) 0;
		box-shadow: var(--shadow);
		font-family: var(--font-mono);
		font-size: var(--text-sm);
		animation: in var(--motion-base) var(--ease-out);
	}
	.toast.bad {
		border-left-color: var(--danger);
	}
	.toast span {
		flex: 1;
	}
	.x {
		background: none;
		border: 0;
		border-radius: var(--radius-sm);
		color: var(--fg-quiet);
		font-size: 18px;
		line-height: 1;
		cursor: pointer;
		padding: 0 4px;
	}
	.x:hover {
		color: var(--fg);
	}
	@keyframes in {
		from {
			transform: translateY(6px);
			opacity: 0;
		}
	}
</style>
