<script lang="ts">
	/** A read-only value (URL, DNS record) with a copy button. */
	let {
		label,
		value,
		hint,
		multiline = false
	}: { label: string; value: string; hint?: string; multiline?: boolean } = $props();
	const uid = $props.id();
	let copied = $state(false);
	let timer: ReturnType<typeof setTimeout>;
	let input = $state<HTMLInputElement | HTMLTextAreaElement>();

	async function copy() {
		try {
			await navigator.clipboard.writeText(value);
		} catch {
			// Clipboard blocked (http, permissions): select it for Ctrl+C.
			input?.select();
			return;
		}
		copied = true;
		clearTimeout(timer);
		timer = setTimeout(() => (copied = false), 1600);
	}
</script>

<div class="copy">
	<label class="lbl" for="cf-{uid}">{label}</label>
	<div class="row">
		{#if multiline}
			<textarea
				bind:this={input}
				id="cf-{uid}"
				readonly
				rows="4"
				{value}
				onfocus={(e) => e.currentTarget.select()}
				spellcheck="false"></textarea>
		{:else}
			<input
				bind:this={input}
				id="cf-{uid}"
				type="text"
				readonly
				{value}
				onfocus={(e) => e.currentTarget.select()}
				spellcheck="false"
			/>
		{/if}
		<button class="btn btn-small" type="button" onclick={copy} aria-label="Copy {label}">
			{copied ? 'Copied' : 'Copy'}
		</button>
	</div>
	{#if hint}<small>{hint}</small>{/if}
	<span class="sr-only" aria-live="polite">{copied ? `${label} copied` : ''}</span>
</div>

<style>
	.copy {
		display: grid;
		gap: var(--space-1);
		min-width: 0;
	}
	.lbl {
		font-family: var(--font-mono);
		font-size: var(--text-xs);
		color: var(--fg-quiet);
	}
	.row {
		display: flex;
		gap: var(--space-2);
		align-items: stretch;
	}
	input {
		flex: 1;
		min-width: 0;
		min-height: 32px;
		font-size: var(--text-sm);
		background: var(--surface-raised);
		color: var(--fg);
	}
	textarea {
		flex: 1;
		min-width: 0;
		padding: var(--space-2) var(--space-3);
		border: 1px solid var(--border);
		border-radius: var(--radius-sm);
		background: var(--surface-raised);
		color: var(--fg);
		font-family: var(--font-mono);
		font-size: 11px;
		line-height: 1.4;
		resize: vertical;
	}
	.row .btn {
		align-self: flex-start;
		min-height: 32px;
		min-width: 64px;
	}
	small {
		font-family: var(--font-mono);
		font-size: var(--text-xs);
		color: var(--fg-quiet);
	}
</style>
