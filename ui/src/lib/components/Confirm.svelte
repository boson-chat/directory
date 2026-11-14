<script lang="ts">
	import type { Snippet } from 'svelte';

	let {
		title,
		action,
		danger = false,
		typed,
		onconfirm,
		oncancel,
		children,
		trigger
	}: {
		title: string;
		action: string;
		danger?: boolean;
		/** When set, the admin must type this exactly before confirming. */
		typed?: string;
		onconfirm: () => Promise<void> | void;
		/** Called when the dialog closes without confirming (Cancel, ×, Escape). */
		oncancel?: () => void;
		children: Snippet;
		trigger: Snippet<[() => void]>;
	} = $props();

	const uid = $props.id();
	let dialog: HTMLDialogElement;
	let busy = $state(false);
	let confirmed = false;
	let entered = $state('');
	let ready = $derived(!typed || entered.trim() === typed);

	function open() {
		confirmed = false;
		entered = '';
		dialog.showModal();
	}
	async function confirm() {
		if (!ready) return;
		busy = true;
		try {
			await onconfirm();
			confirmed = true;
			dialog.close();
		} catch {
			// onconfirm reported the failure; keep the dialog open to retry.
		} finally {
			busy = false;
		}
	}
	function onclose() {
		if (!confirmed) oncancel?.();
	}
</script>

{@render trigger(open)}

<dialog bind:this={dialog} aria-labelledby="dlg-{uid}" {onclose}>
	<form method="dialog">
		<header>
			<h2 id="dlg-{uid}">{title}</h2>
			<button class="close" value="cancel" aria-label="Close" disabled={busy}>×</button>
		</header>
		<div class="body">
			{@render children()}
			{#if typed}
				<label class="field typed">
					<span>Type <code>{typed}</code> to confirm</span>
					<input
						type="text"
						bind:value={entered}
						autocomplete="off"
						spellcheck="false"
						autocapitalize="off"
						onkeydown={(e) => {
							if (e.key === 'Enter') {
								e.preventDefault();
								confirm();
							}
						}}
					/>
				</label>
			{/if}
		</div>
		<div class="actions">
			<button class="btn btn-ghost" value="cancel" disabled={busy}>Cancel</button>
			<button
				class="btn {danger ? 'btn-danger' : 'btn-primary'}"
				type="button"
				onclick={confirm}
				disabled={busy || !ready}>{busy ? 'Working…' : action}</button
			>
		</div>
	</form>
</dialog>

<style>
	/* boson Modal */
	dialog {
		width: min(460px, calc(100vw - 32px));
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
		overflow-wrap: anywhere;
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
		color: var(--fg-2);
		font-size: var(--text-base);
	}
	.typed {
		margin-top: var(--space-4);
	}
	.typed > span {
		text-transform: none;
		letter-spacing: 0;
	}
	.actions {
		display: flex;
		justify-content: flex-end;
		gap: var(--space-2);
		padding: 0 var(--space-5) var(--space-5);
	}
	@keyframes rise {
		from {
			opacity: 0;
			transform: translateY(8px) scale(0.985);
		}
	}
</style>
