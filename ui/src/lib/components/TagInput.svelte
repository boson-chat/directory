<script lang="ts">
	import { normaliseTag, LIMITS } from '$lib/validate';

	/**
	 * Free-form entries as removable chips. Enter, comma or Tab adds the typed
	 * entry. Topics by default; `normalise`/`key`/`max` make it a channel list.
	 */
	let {
		tags = $bindable([]),
		id,
		describedby,
		labelledby,
		invalid = false,
		normalise = normaliseTag,
		key = (t: string) => t,
		max = LIMITS.tags,
		placeholder = 'gaming, linux, music',
		full = 'That’s the most tags'
	}: {
		tags?: string[];
		id?: string;
		describedby?: string;
		labelledby?: string;
		invalid?: boolean;
		/** Returns the entry to add, or '' when the text can't be one (it stays in the box). */
		normalise?: (raw: string) => string;
		/** Duplicate check: entries with the same key are one entry. */
		key?: (t: string) => string;
		max?: number;
		placeholder?: string;
		full?: string;
	} = $props();
	let draft = $state('');
	let input = $state<HTMLInputElement>();

	function add() {
		const t = normalise(draft);
		if (!t) {
			if (!draft.trim()) draft = '';
			return;
		}
		draft = '';
		if (!tags.some((x) => key(x) === key(t)) && tags.length < max) tags = [...tags, t];
	}
	function remove(t: string) {
		tags = tags.filter((x) => x !== t);
		input?.focus();
	}
	function onkeydown(e: KeyboardEvent) {
		if (e.key === 'Enter' || e.key === ',' || (e.key === 'Tab' && draft.trim())) {
			e.preventDefault();
			add();
		} else if (e.key === 'Backspace' && !draft && tags.length) {
			tags = tags.slice(0, -1);
		}
	}
</script>

<div class="tags" class:invalid>
	{#each tags as t (t)}
		<span class="chip">
			{t}
			<button type="button" aria-label="Remove {t}" onclick={() => remove(t)}>×</button>
		</span>
	{/each}
	<input
		bind:this={input}
		{id}
		type="text"
		bind:value={draft}
		{onkeydown}
		onblur={add}
		placeholder={tags.length >= max ? full : tags.length ? 'Add another' : placeholder}
		disabled={tags.length >= max}
		aria-describedby={describedby}
		aria-labelledby={labelledby}
		aria-invalid={invalid || undefined}
		autocomplete="off"
		spellcheck="false"
		autocapitalize="none"
	/>
</div>

<style>
	.tags {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: var(--space-1);
		min-height: 38px;
		padding: 4px;
		border: 1px solid var(--border);
		border-radius: var(--radius-sm);
		background: var(--surface);
		transition: border-color var(--motion-fast) var(--ease-standard);
	}
	.tags:hover {
		border-color: var(--border-strong);
	}
	.tags:focus-within {
		border-color: var(--accent);
		box-shadow:
			0 0 0 1px var(--accent),
			0 0 0 4px var(--accent-dim);
	}
	.invalid {
		border-color: var(--danger);
	}
	.chip {
		display: inline-flex;
		align-items: center;
		gap: 2px;
		padding: 2px 2px 2px 8px;
		border-radius: var(--radius-sm);
		background: var(--surface-hover);
		box-shadow: inset 0 0 0 1px var(--border);
		font-family: var(--font-mono);
		font-size: var(--text-sm);
		color: var(--fg);
	}
	.chip button {
		background: none;
		border: 0;
		border-radius: var(--radius-sm);
		color: var(--fg-quiet);
		cursor: pointer;
		font-size: 15px;
		line-height: 1;
		padding: 2px 5px;
	}
	.chip button:hover {
		color: var(--fg);
		background: var(--surface-raised);
	}
	input {
		flex: 1;
		min-width: 12ch;
		min-height: 28px;
		border: 0;
		background: transparent;
		padding: 0 var(--space-2);
		box-shadow: none;
		font-size: var(--text-sm);
	}
	input:focus-visible {
		box-shadow: none;
		border: 0;
	}
</style>
