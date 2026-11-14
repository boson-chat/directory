<!--
	Dropdown: the panel's select. A WAI-ARIA "select-only combobox"
	(https://www.w3.org/WAI/ARIA/apg/patterns/combobox/examples/combobox-select-only/):
	focus stays on the trigger (role=combobox) and the highlighted option is
	announced through aria-activedescendant.

	Keys, closed: ↓ ↑ Enter Space Alt+↓ open; Home / End open on the first / last
	option; typing opens and jumps to a matching option.
	Keys, open: ↓ ↑ PageDown PageUp Home End move; Enter Space Alt+↑ choose;
	Tab chooses and moves on; Escape closes without changing anything; typing
	jumps (type-ahead).

	With `multiple` it is a multi-select listbox (aria-multiselectable):
	Enter, Space and clicks toggle an option and keep the list open; closing
	it (Escape, Tab, a click outside, the trigger) reports the new `values`
	once through `onchangemany`.

	No inline style attributes: placement is classes plus two CSS custom
	properties set through the CSSOM.
-->
<script lang="ts" generics="V extends string">
	import { tick } from 'svelte';
	import { edge, match, place, step, type DropdownOption } from '$lib/dropdown';

	let {
		options,
		value = $bindable(),
		onchange,
		id,
		name,
		label,
		labelledby,
		describedby,
		placeholder = 'Choose…',
		disabled = false,
		size = 'md',
		align = 'start',
		title,
		class: className = '',
		multiple = false,
		values = $bindable([]),
		onchangemany,
		summary
	}: {
		options: DropdownOption<V>[];
		value?: V;
		/** Called after the user picks a different option. */
		onchange?: (value: V) => void;
		id?: string;
		/** Submits the value with a surrounding <form> through a hidden input. */
		name?: string;
		/** Accessible name when there is no visible label to point at. */
		label?: string;
		/** Id of the visible label. */
		labelledby?: string;
		describedby?: string;
		placeholder?: string;
		disabled?: boolean;
		size?: 'md' | 'small';
		/** Preferred horizontal alignment of the list (flips if it would overflow). */
		align?: 'start' | 'end';
		title?: string;
		class?: string;
		/** Multi-select: the chosen options are `values`. */
		multiple?: boolean;
		values?: V[];
		/** Multi-select: called once when the list closes, if the choice changed. */
		onchangemany?: (values: V[]) => void;
		/** Multi-select: the trigger's text for the chosen options. */
		summary?: (chosen: DropdownOption<V>[]) => string;
	} = $props();

	const uid = $props.id();
	const baseId = $derived(id ?? 'dd-' + uid);
	const listId = $derived(baseId + '-list');
	const optId = (i: number) => `${baseId}-opt-${i}`;

	let open = $state(false);
	let active = $state(-1);
	let up = $state(false);
	let end = $state(false);

	let root: HTMLDivElement;
	let trigger: HTMLButtonElement;
	let list: HTMLDivElement;

	let selectedIndex = $derived(
		multiple
			? options.findIndex((o) => values.includes(o.value))
			: options.findIndex((o) => o.value === value)
	);
	let selected = $derived(selectedIndex >= 0 ? options[selectedIndex] : undefined);
	let chosen = $derived(multiple ? options.filter((o) => values.includes(o.value)) : []);
	let triggerText = $derived(
		multiple
			? chosen.length
				? (summary?.(chosen) ?? chosen.map((o) => o.label).join(', '))
				: ''
			: (selected?.label ?? '')
	);
	const isSelected = (i: number) => (multiple ? values.includes(options[i].value) : i === selectedIndex);
	// Multi-select: the values when the list opened, to report a change on close.
	let openedWith: V[] = [];

	// Consecutive options sharing a group render under one heading.
	let sections = $derived.by(() => {
		const out: { group?: string; items: { o: DropdownOption<V>; i: number }[] }[] = [];
		options.forEach((o, i) => {
			const last = out[out.length - 1];
			if (last && last.group === o.group) last.items.push({ o, i });
			else out.push({ group: o.group, items: [{ o, i }] });
		});
		return out;
	});

	let typed = '';
	let typedAt = 0;

	async function show(highlight: number) {
		if (disabled || open) return;
		open = true;
		active = highlight;
		openedWith = [...values];
		await tick();
		position();
		scrollActive();
	}

	function hide(refocus = true) {
		if (!open) return;
		open = false;
		active = -1;
		if (refocus) trigger?.focus();
		if (multiple) {
			const now = [...values];
			if (now.length !== openedWith.length || now.some((v) => !openedWith.includes(v))) onchangemany?.(now);
		}
	}

	function choose(i: number) {
		const o = options[i];
		if (!o || o.disabled) return;
		if (multiple) {
			values = values.includes(o.value) ? values.filter((v) => v !== o.value) : [...values, o.value];
			active = i;
			return;
		}
		const changed = o.value !== value;
		value = o.value;
		hide();
		if (changed) onchange?.(o.value);
	}

	function position() {
		if (!open || !list || !trigger) return;
		list.style.removeProperty('--dd-max-h');
		list.style.removeProperty('--dd-max-w');
		const p = place(
			trigger.getBoundingClientRect(),
			{ width: list.offsetWidth, height: list.scrollHeight },
			{ width: document.documentElement.clientWidth, height: window.innerHeight },
			align
		);
		up = p.up;
		end = p.end;
		list.style.setProperty('--dd-max-h', p.maxHeight + 'px');
		list.style.setProperty('--dd-max-w', p.maxWidth + 'px');
	}

	function scrollActive() {
		if (active < 0 || !list) return;
		list.querySelector<HTMLElement>('#' + CSS.escape(optId(active)))?.scrollIntoView({ block: 'nearest' });
	}

	function move(i: number) {
		if (i < 0) return;
		active = i;
		tick().then(scrollActive);
	}

	function typeahead(ch: string) {
		const now = Date.now();
		typed = now - typedAt > 600 ? ch : typed + ch;
		typedAt = now;
		const from = open ? active : selectedIndex;
		const i = match(options, typed, from);
		if (i < 0) return;
		if (open) move(i);
		else show(i);
	}

	function startIndex() {
		return selectedIndex >= 0 && !options[selectedIndex].disabled ? selectedIndex : edge(options, 1);
	}

	function onkeydown(e: KeyboardEvent) {
		if (disabled) return;
		const k = e.key;
		if (!open) {
			if (k === 'ArrowDown' || k === 'ArrowUp' || k === 'Enter' || k === ' ') {
				e.preventDefault();
				show(startIndex());
			} else if (k === 'Home' || k === 'End') {
				e.preventDefault();
				show(edge(options, k === 'Home' ? 1 : -1));
			} else if (k.length === 1 && k !== ' ' && !e.ctrlKey && !e.metaKey && !e.altKey) {
				typeahead(k);
			}
			return;
		}
		switch (k) {
			case 'ArrowDown':
				e.preventDefault();
				move(active < 0 ? edge(options, 1) : step(options, active, 1));
				break;
			case 'ArrowUp':
				e.preventDefault();
				if (e.altKey) choose(active);
				else move(active < 0 ? edge(options, -1) : step(options, active, -1));
				break;
			case 'PageDown':
				e.preventDefault();
				move(step(options, Math.max(active, 0), 1, 10));
				break;
			case 'PageUp':
				e.preventDefault();
				move(step(options, Math.max(active, 0), -1, 10));
				break;
			case 'Home':
				e.preventDefault();
				move(edge(options, 1));
				break;
			case 'End':
				e.preventDefault();
				move(edge(options, -1));
				break;
			case 'Enter':
			case ' ':
				// A space mid-type-ahead is part of the query ("last 7 days").
				if (k === ' ' && typed && Date.now() - typedAt < 600) {
					e.preventDefault();
					typeahead(' ');
					break;
				}
				e.preventDefault();
				choose(active);
				break;
			case 'Escape':
				e.preventDefault();
				e.stopPropagation(); // don't also close a surrounding dialog
				hide();
				break;
			case 'Tab':
				// Choose and let focus move on (multi-select: just close).
				if (!multiple && active >= 0 && active !== selectedIndex) choose(active);
				hide(false);
				break;
			default:
				if (k.length === 1 && !e.ctrlKey && !e.metaKey && !e.altKey) typeahead(k);
		}
	}

	function onclick() {
		if (disabled) return;
		if (open) hide();
		else show(startIndex());
	}

	// Close when focus leaves the widget (Tab is handled above).
	function onfocusout(e: FocusEvent) {
		const to = e.relatedTarget as Node | null;
		if (open && (!to || !root.contains(to))) hide(false);
	}

	// Click outside closes.
	$effect(() => {
		if (!open) return;
		const down = (e: PointerEvent) => {
			if (!root.contains(e.target as Node)) hide(false);
		};
		const reflow = () => position();
		document.addEventListener('pointerdown', down, true);
		window.addEventListener('resize', reflow);
		window.addEventListener('scroll', reflow, { passive: true, capture: true });
		return () => {
			document.removeEventListener('pointerdown', down, true);
			window.removeEventListener('resize', reflow);
			window.removeEventListener('scroll', reflow, { capture: true });
		};
	});

	// Disabling while open closes.
	$effect(() => {
		if (disabled) hide(false);
	});
</script>

<div class="dd {className}" class:open class:small={size === 'small'} bind:this={root} {onfocusout}>
	{#if name}<input type="hidden" {name} value={value ?? ''} />{/if}
	<button
		bind:this={trigger}
		type="button"
		id={baseId}
		class="trigger"
		role="combobox"
		aria-haspopup="listbox"
		aria-expanded={open}
		aria-controls={listId}
		aria-activedescendant={open && active >= 0 ? optId(active) : undefined}
		aria-label={label}
		aria-labelledby={labelledby}
		aria-describedby={describedby}
		{disabled}
		{title}
		{onclick}
		{onkeydown}
	>
		<span class="value" class:placeholder={!triggerText}>{triggerText || placeholder}</span>
		<span class="chev" aria-hidden="true"></span>
	</button>
	<div
		bind:this={list}
		id={listId}
		class="list"
		class:up
		class:end
		role="listbox"
		aria-multiselectable={multiple || undefined}
		tabindex="-1"
		aria-label={label}
		aria-labelledby={labelledby}
		hidden={!open}
		onmousedown={(e) => e.preventDefault()}
	>
		{#each sections as sec, s (s)}
			<ul
				class="section"
				role={sec.group ? 'group' : 'presentation'}
				aria-labelledby={sec.group ? `${baseId}-g${s}` : undefined}
			>
				{#if sec.group}
					<li role="presentation" class="group-label" id="{baseId}-g{s}">{sec.group}</li>
				{/if}
				{#each sec.items as { o, i } (i)}
					<!-- keys are handled on the combobox, which keeps focus (aria-activedescendant) -->
					<!-- svelte-ignore a11y_click_events_have_key_events -->
					<li
						id={optId(i)}
						role="option"
						class="opt"
						class:multi={multiple}
						class:active={i === active}
						aria-selected={isSelected(i)}
						aria-disabled={o.disabled || undefined}
						aria-labelledby={o.description ? `${optId(i)}-l` : undefined}
						aria-describedby={o.description ? `${optId(i)}-d` : undefined}
						onclick={() => choose(i)}
						onpointermove={() => {
							if (!o.disabled && active !== i) active = i;
						}}
					>
						{#if multiple}<span class="check" aria-hidden="true"></span>{/if}
						<span class="opt-label" id="{optId(i)}-l">{o.label}</span>
						{#if o.description}<span class="opt-desc" id="{optId(i)}-d">{o.description}</span>{/if}
					</li>
				{/each}
			</ul>
		{/each}
	</div>
</div>

<style>
	.dd {
		position: relative;
		min-width: 0;
	}
	.trigger {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: var(--space-3);
		width: 100%;
		min-height: 38px;
		padding: var(--space-2) var(--space-3);
		border-radius: var(--radius-sm);
		border: 1px solid var(--border);
		background: var(--surface);
		color: var(--fg);
		font-family: var(--font-mono);
		font-size: var(--text-base);
		line-height: 1.4;
		text-align: left;
		cursor: pointer;
		transition:
			border-color var(--motion-fast) var(--ease-standard),
			box-shadow var(--motion-fast) var(--ease-standard);
	}
	.trigger:hover:not(:disabled) {
		border-color: var(--border-strong);
	}
	.trigger:focus-visible,
	.open .trigger {
		outline: none;
		border-color: var(--accent);
		box-shadow:
			0 0 0 1px var(--accent),
			0 0 0 4px var(--accent-dim);
	}
	.trigger:disabled {
		opacity: 0.45;
		cursor: not-allowed;
	}
	.value {
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.placeholder {
		color: var(--fg-3);
	}
	/* chevron: two strokes, turns over when open */
	.chev {
		flex: none;
		width: 7px;
		height: 7px;
		margin: -3px 2px 0;
		border-right: 1.5px solid var(--fg-2);
		border-bottom: 1.5px solid var(--fg-2);
		transform: rotate(45deg);
		transition: transform var(--motion-base) var(--ease-standard);
	}
	.open .chev {
		transform: translateY(3px) rotate(225deg);
		border-color: var(--accent);
	}

	.small .trigger {
		min-height: 28px;
		padding: var(--space-1) var(--space-2) var(--space-1) var(--space-3);
		font-size: var(--text-xs);
		gap: var(--space-2);
	}
	.small .chev {
		width: 6px;
		height: 6px;
	}

	.list {
		position: absolute;
		z-index: 40;
		top: calc(100% + 6px);
		left: 0;
		min-width: 100%;
		width: max-content;
		max-width: min(var(--dd-max-w, 360px), 360px, calc(100vw - 16px));
		max-height: var(--dd-max-h, 320px);
		overflow-y: auto;
		overscroll-behavior: contain;
		padding: var(--space-1);
		background: var(--surface-raised);
		border: 1px solid var(--border-strong);
		border-radius: var(--radius-sm);
		box-shadow: var(--shadow);
		outline: none;
		animation: drop var(--motion-base) var(--ease-out);
	}
	.list[hidden] {
		display: none;
	}
	.list.up {
		top: auto;
		bottom: calc(100% + 6px);
		animation-name: lift;
	}
	.list.end {
		left: auto;
		right: 0;
	}
	.section {
		margin: 0;
		padding: 0;
		list-style: none;
	}
	.section + .section {
		margin-top: var(--space-1);
		padding-top: var(--space-1);
		border-top: 1px solid var(--border-soft);
	}
	.group-label {
		padding: var(--space-2) var(--space-3) var(--space-1);
		font-family: var(--font-mono);
		font-size: 10px;
		font-weight: 600;
		letter-spacing: var(--tracking-label);
		text-transform: uppercase;
		color: var(--fg-quiet);
		cursor: default;
	}
	.opt {
		display: grid;
		gap: 2px;
		padding: var(--space-2) var(--space-3);
		border-left: 2px solid transparent;
		border-radius: var(--radius-sm);
		color: var(--fg-2);
		font-family: var(--font-mono);
		font-size: var(--text-sm);
		line-height: 1.35;
		cursor: pointer;
		overflow-wrap: anywhere;
	}
	.small .opt {
		font-size: var(--text-xs);
	}
	.opt.active {
		background: var(--surface-hover);
		color: var(--fg);
	}
	/* the chosen option: amber rule and ink, like the rail's current page */
	.opt[aria-selected='true'] {
		color: var(--accent-text);
		border-left-color: var(--accent);
		border-radius: 0 var(--radius-sm) var(--radius-sm) 0;
		font-weight: 600;
	}
	/* multi-select: a check box in front, no amber rule (several can be on) */
	.opt.multi {
		grid-template-columns: 14px minmax(0, 1fr);
		column-gap: var(--space-3);
	}
	.opt.multi .opt-desc {
		grid-column: 2;
	}
	.opt.multi[aria-selected='true'] {
		border-left-color: transparent;
		border-radius: var(--radius-sm);
		color: var(--fg);
	}
	.check {
		width: 14px;
		height: 14px;
		margin-top: 1px;
		border-radius: var(--radius-sm);
		box-shadow: inset 0 0 0 1px var(--border-strong);
		position: relative;
	}
	.opt[aria-selected='true'] .check {
		background: var(--accent);
		box-shadow: none;
	}
	.opt[aria-selected='true'] .check::after {
		content: '';
		position: absolute;
		left: 4.5px;
		top: 1.5px;
		width: 4px;
		height: 8px;
		border-right: 2px solid var(--accent-on);
		border-bottom: 2px solid var(--accent-on);
		transform: rotate(45deg);
	}
	.opt[aria-disabled='true'] .check {
		opacity: 0.5;
	}
	.opt[aria-disabled='true'] {
		color: var(--fg-3);
		cursor: not-allowed;
		background: none;
	}
	.opt-desc {
		font-family: var(--font-body);
		font-size: var(--text-xs);
		font-weight: 400;
		color: var(--fg-quiet);
	}
	.opt[aria-disabled='true'] .opt-desc {
		color: var(--fg-3);
	}
	@keyframes drop {
		from {
			opacity: 0;
			transform: translateY(-4px);
		}
	}
	@keyframes lift {
		from {
			opacity: 0;
			transform: translateY(4px);
		}
	}
	@media (prefers-reduced-motion: reduce) {
		.list,
		.list.up {
			animation: none;
		}
		.chev {
			transition: none;
		}
	}
	@media (forced-colors: active) {
		.opt.active {
			outline: 2px solid Highlight;
		}
		.opt[aria-selected='true'] {
			border-left-color: Highlight;
		}
	}
</style>
