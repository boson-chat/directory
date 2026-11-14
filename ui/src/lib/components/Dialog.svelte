<script lang="ts">
	import type { Snippet } from 'svelte';

	/** A modal with a title and free content (forms). Call open()/close(). */
	let { title, children, onclose }: { title: string; children: Snippet; onclose?: () => void } = $props();
	const uid = $props.id();
	let dialog: HTMLDialogElement;

	export function open() {
		dialog.showModal();
	}
	export function close() {
		dialog.close();
	}
</script>

<dialog bind:this={dialog} aria-labelledby="dlg-{uid}" {onclose}>
	<header>
		<h2 id="dlg-{uid}">{title}</h2>
		<button class="close" type="button" aria-label="Close" onclick={close}>×</button>
	</header>
	<div class="body">{@render children()}</div>
</dialog>

<style>
	dialog {
		width: min(520px, calc(100vw - 32px));
		max-height: calc(100dvh - 32px);
		padding: 0;
		border: 1px solid var(--border);
		border-radius: var(--radius-md);
		background: var(--surface);
		color: var(--fg);
		box-shadow: var(--shadow);
	}
	dialog[open] {
		animation: rise var(--motion-medium) var(--ease-in-out) both;
	}
	dialog::backdrop {
		background: var(--backdrop);
	}
	header {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-3);
		padding: var(--space-4) var(--space-5);
		border-bottom: 1px solid var(--border);
	}
	h2 {
		font-size: var(--text-lg);
	}
	.close {
		flex: none;
		width: 28px;
		height: 28px;
		display: inline-flex;
		align-items: center;
		justify-content: center;
		background: none;
		border: 0;
		border-radius: var(--radius-sm);
		color: var(--fg-quiet);
		font-size: var(--text-xl);
		line-height: 1;
		cursor: pointer;
	}
	.close:hover {
		color: var(--fg);
		background: var(--surface-hover);
	}
	.body {
		padding: var(--space-5);
	}
	@keyframes rise {
		from {
			opacity: 0;
			transform: translateY(8px) scale(0.985);
		}
	}
</style>
