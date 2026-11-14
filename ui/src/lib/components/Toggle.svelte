<script lang="ts">
	/** An on/off switch (a checkbox with role="switch"). */
	let {
		checked = $bindable(false),
		label,
		detail,
		disabled = false,
		onchange
	}: {
		checked?: boolean;
		label: string;
		detail?: string;
		disabled?: boolean;
		onchange?: (v: boolean) => void;
	} = $props();
</script>

<label class="toggle" class:disabled>
	<span class="txt">
		<span class="label">{label}</span>
		{#if detail}<span class="detail">{detail}</span>{/if}
	</span>
	<input type="checkbox" role="switch" bind:checked {disabled} onchange={() => onchange?.(checked)} />
	<span class="track" aria-hidden="true"><span class="knob"></span></span>
</label>

<style>
	.toggle {
		display: grid;
		grid-template-columns: minmax(0, 1fr) auto;
		gap: var(--space-4);
		align-items: center;
		cursor: pointer;
		position: relative;
	}
	.disabled {
		cursor: not-allowed;
		opacity: 0.6;
	}
	.txt {
		display: grid;
		gap: 2px;
	}
	.label {
		font-family: var(--font-mono);
		font-weight: 600;
		font-size: var(--text-sm);
		color: var(--fg);
	}
	.detail {
		font-size: var(--text-sm);
		color: var(--fg-2);
		line-height: 1.5;
	}
	input {
		position: absolute;
		opacity: 0;
		width: 1px;
		height: 1px;
		right: 0;
	}
	.track {
		width: 36px;
		height: 20px;
		border-radius: 999px;
		background: var(--surface-hover);
		box-shadow: inset 0 0 0 1px var(--border-strong);
		position: relative;
		transition: background-color var(--motion-fast) var(--ease-standard);
	}
	.knob {
		position: absolute;
		top: 3px;
		left: 3px;
		width: 14px;
		height: 14px;
		border-radius: 50%;
		background: var(--fg-2);
		transition:
			transform var(--motion-base) var(--ease-out),
			background-color var(--motion-fast) var(--ease-standard);
	}
	input:checked + .track {
		background: var(--accent);
		box-shadow: none;
	}
	input:checked + .track .knob {
		transform: translateX(16px);
		background: var(--accent-on);
	}
	input:focus-visible + .track {
		box-shadow: var(--focus-ring);
	}
</style>
