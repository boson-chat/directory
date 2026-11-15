<script lang="ts">
	import { directory } from '$lib/api';
	import type { Listing } from '$lib/types';
	import { errorText, toasts } from '$lib/toasts.svelte';
	import {
		cleanTheme,
		contrastIssues,
		otherBase,
		MAX_BLUR,
		MAX_RADIUS,
		PANES,
		paneDefault,
		POSITIONS,
		readTheme,
		resolveColors,
		resolveNick,
		THEME_TOKENS,
		themeFor,
		type BosonTheme,
		type ThemeBase,
		type ThemeVariant,
		type ImageSlot,
		type PaneName,
		type PanePosition,
		type ThemePane,
		type ThemeToken
	} from '$lib/theme';
	import type { DropdownOption } from '$lib/dropdown';
	import Dropdown from './Dropdown.svelte';
	import Confirm from './Confirm.svelte';
	import ThemeImage from './ThemeImage.svelte';
	import ThemePreview from './ThemePreview.svelte';

	/**
	 * The listing's boson-theme/1 editor. Colours, panes and type are a draft
	 * until "Save theme"; images upload at once (each is its own slot).
	 *
	 * Dark | Light picks the version being edited. The theme's `base` is the
	 * primary version; the other one, when provided, is `variants.<other>`:
	 * its own colours, nick colours and pane overrides (images, radius and
	 * font are shared).
	 */
	let { listing = $bindable() }: { listing: Listing } = $props();

	const fresh = (): BosonTheme => ({ version: 1, base: 'dark' });
	const copy = (t: BosonTheme): BosonTheme => JSON.parse(JSON.stringify(t));
	let saved = $derived(readTheme(listing.network.theme));
	// The draft starts from what the server has; edits stay local until saved.
	let draft = $state<BosonTheme>(copy(readTheme(listing.network.theme) ?? fresh()));
	let busy = $state(false);
	// The version being edited; the primary (base) first.
	let editing = $state<ThemeBase>(readTheme(listing.network.theme)?.base ?? 'dark');

	let primary = $derived(editing === draft.base);
	let other = $derived(otherBase(draft.base));
	let hasOther = $derived(!!draft.variants?.[other]);
	// Editable: the primary, or the other version once it exists.
	let editable = $derived(primary || hasOther);
	// What people in `editing` mode see: the variant merged over the primary.
	let view = $derived(themeFor(draft, editing));
	let colors = $derived(resolveColors(view));
	let nick = $derived(resolveNick(view));
	let issues = $derived(contrastIssues(view));
	const canon = (t: BosonTheme) => JSON.stringify(readTheme(cleanTheme(t)));
	let dirty = $derived(canon(draft) !== canon(saved ?? fresh()));
	const modeName = (b: ThemeBase) => (b === 'dark' ? 'dark' : 'light');
	const groups = [...new Set(THEME_TOKENS.map((t) => t.group))];
	const positions: DropdownOption<PanePosition>[] = POSITIONS.map((p) => ({
		value: p,
		label: p[0].toUpperCase() + p.slice(1)
	}));
	const paneLabels: Record<PaneName, string> = {
		servers: 'Server rail',
		channels: 'Channel list',
		messages: 'Messages',
		members: 'Member list'
	};

	/** Where edits to colours, nick and panes go: the primary or the variant. */
	function look(): ThemeVariant {
		if (primary) return draft;
		draft.variants ??= {};
		return (draft.variants[editing] ??= {});
	}
	/** The edited version's own settings (not inherited ones). */
	let own = $derived<ThemeVariant>((primary ? draft : draft.variants?.[editing]) ?? {});

	function pick(b: ThemeBase) {
		editing = b;
		// A theme with nothing in it yet: the first pick is its base.
		const empty = !Object.keys(cleanTheme(draft)).some((k) => !['version', 'base'].includes(k));
		if (empty) draft.base = b;
	}
	function addVariant() {
		// Starts from Boson's own look for that base (no colours of its own).
		draft.variants = { [editing]: {} };
	}
	function removeVariant() {
		delete draft.variants;
	}
	function makePrimary() {
		// Only offered without a variant: the colours move with it.
		draft.base = editing;
	}

	function setColor(key: ThemeToken, value: string) {
		const l = look();
		l.colors = { ...(l.colors ?? {}), [key]: value.toLowerCase() };
	}
	function clearColor(key: ThemeToken) {
		const l = look();
		if (!l.colors) return;
		const next = { ...l.colors };
		delete next[key];
		l.colors = next;
	}
	function setNick(key: 'saturation' | 'lightness', value: number) {
		const l = look();
		l.nick = { ...l.nick, [key]: value };
	}
	function pane(l: ThemeVariant, name: PaneName) {
		l.panes ??= {};
		l.panes[name] ??= {};
		return l.panes[name]!;
	}
	function setPane<K extends keyof ThemePane>(
		name: PaneName,
		key: K,
		value: ThemePane[K] | undefined,
		l: ThemeVariant = look()
	) {
		const p = pane(l, name);
		if (value === undefined) delete p[key];
		else p[key] = value;
	}
	/** An uploaded image is live at once: mirror it into the draft (images are the primary's). */
	function imageChanged(slot: ImageSlot, url: string | undefined) {
		if (slot.startsWith('pane-')) setPane(slot.slice(5) as PaneName, 'image', url, draft);
		else if (url) draft[slot as 'logo'] = url;
		else delete draft[slot as 'logo'];
	}

	async function save() {
		busy = true;
		try {
			listing = await directory.putTheme(listing.network.id, cleanTheme(draft));
			draft = copy(readTheme(listing.network.theme) ?? fresh());
			toasts.push('Theme saved. Clients pick it up the next time they connect.');
		} catch (e) {
			toasts.push(errorText(e), 'bad');
		} finally {
			busy = false;
		}
	}
	function discard() {
		draft = copy(saved ?? fresh());
	}
	async function removeTheme() {
		try {
			listing = await directory.deleteTheme(listing.network.id);
			draft = fresh();
			editing = 'dark';
			toasts.push('Theme removed.');
		} catch (e) {
			toasts.push(errorText(e), 'bad');
			throw e;
		}
	}
	const ratio = (r: number) => r.toFixed(r < 10 ? 1 : 0) + ':1';
</script>

<div class="editor">
	<div class="controls">
		<fieldset>
			<legend>Mode</legend>
			<div class="seg" role="radiogroup" aria-label="Version to edit">
				{#each ['dark', 'light'] as const as b (b)}
					<button
						type="button"
						role="radio"
						aria-checked={editing === b}
						class:on={editing === b}
						onclick={() => pick(b)}
						>{b === 'dark' ? 'Dark' : 'Light'}{#if b === draft.base && (hasOther || editing !== b)}<span
								class="tag">primary</span
							>{:else if b !== draft.base && hasOther}<span class="tag">added</span>{/if}</button
					>
				{/each}
			</div>
			{#if primary}
				<p class="hint">
					Colours you don’t set come from Boson’s own {modeName(draft.base)} theme.
					{#if !hasOther}People in {modeName(other)} mode see this version too.{/if}
				</p>
			{:else if hasOther}
				<div class="variant-bar">
					<p class="hint">
						The {modeName(editing)} version, for people in {modeName(editing)} mode. Colours you don’t set come
						from Boson’s own {modeName(editing)} theme; images, font and corners are shared with the
						{modeName(draft.base)} version.
					</p>
					<button class="btn btn-ghost btn-small rm" type="button" onclick={removeVariant}
						>Remove {modeName(editing)} version</button
					>
				</div>
			{:else}
				<div class="novariant">
					<p>
						<strong>No {modeName(editing)} version yet.</strong>
						People in {modeName(editing)} mode see your {modeName(draft.base)} theme.
					</p>
					<div class="acts">
						<button class="btn btn-primary" type="button" onclick={addVariant}
							>Also provide a {modeName(editing)} version</button
						>
						<button class="btn btn-ghost" type="button" onclick={makePrimary}
							>Make it a {modeName(editing)} theme instead</button
						>
					</div>
					<p class="hint">
						The {modeName(editing)} version starts from Boson’s own {modeName(editing)} theme. Your
						{modeName(draft.base)} colours stay as they are.
					</p>
				</div>
			{/if}
		</fieldset>

		{#if editable}
			{#each groups as g (g)}
				<fieldset>
					<legend>{g}</legend>
					<div class="swatches">
						{#each THEME_TOKENS.filter((t) => t.group === g) as tok (tok.key)}
							{@const mine = own.colors?.[tok.key]}
							<div class="sw" class:own={mine}>
								<label>
									<input
										type="color"
										value={colors[tok.key]}
										oninput={(e) => setColor(tok.key, e.currentTarget.value)}
										aria-label="{tok.label} colour"
									/>
									<span class="nm">{tok.label}</span>
									<span class="hex">{colors[tok.key]}</span>
								</label>
								{#if mine}
									<button
										type="button"
										class="clear"
										title="Use the base colour"
										aria-label="Reset {tok.label} to the base colour"
										onclick={() => clearColor(tok.key)}>×</button
									>
								{/if}
							</div>
						{/each}
					</div>
				</fieldset>
			{/each}

			{#if issues.length}
				<div class="warn" role="status">
					<strong>Low contrast</strong>
					<ul>
						{#each issues as i (i.label)}
							<li>{i.label}: {ratio(i.ratio)} (needs 4.5:1)</li>
						{/each}
					</ul>
					<p>
						Clients and this directory won’t use text colours that don’t read: they keep their own instead.
					</p>
				</div>
			{/if}
		{/if}

		<fieldset>
			<legend>Type and shape</legend>
			<div class="row">
				<span class="rl" id="font-l">Font</span>
				<div class="seg" role="radiogroup" aria-labelledby="font-l">
					{#each ['boson', 'system'] as const as f (f)}
						<button
							type="button"
							role="radio"
							aria-checked={(draft.font ?? 'boson') === f}
							class:on={(draft.font ?? 'boson') === f}
							onclick={() => (draft.font = f)}>{f === 'boson' ? 'Boson' : 'System'}</button
						>
					{/each}
				</div>
			</div>
			<label class="row">
				<span class="rl">Corner radius</span>
				<input
					type="range"
					min="0"
					max={MAX_RADIUS}
					step="1"
					value={draft.radius ?? 2}
					oninput={(e) => (draft.radius = Number(e.currentTarget.value))}
				/>
				<output>{draft.radius ?? 2} px</output>
			</label>
			{#if editable}
				<label class="row">
					<span class="rl">Nick saturation</span>
					<input
						type="range"
						min="0"
						max="100"
						value={nick.saturation}
						oninput={(e) => setNick('saturation', Number(e.currentTarget.value))}
					/>
					<output>{nick.saturation}%</output>
				</label>
				<label class="row">
					<span class="rl">Nick lightness</span>
					<input
						type="range"
						min="0"
						max="100"
						value={nick.lightness}
						oninput={(e) => setNick('lightness', Number(e.currentTarget.value))}
					/>
					<output>{nick.lightness}%</output>
				</label>
			{/if}
		</fieldset>

		<fieldset>
			<legend>Logo and sign-in</legend>
			<div class="images">
				<ThemeImage
					bind:listing
					slot="logo"
					label="Logo"
					hint="Square mark: server rail, headers, sign-in."
					onchange={(u) => imageChanged('logo', u)}
				/>
				<ThemeImage
					bind:listing
					slot="logo_wide"
					label="Wordmark"
					hint="Optional wide logo for headers and emails."
					onchange={(u) => imageChanged('logo_wide', u)}
				/>
				<ThemeImage
					bind:listing
					slot="login_background"
					label="Sign-in background"
					hint="Behind “Sign in to {listing.network.name}”."
					onchange={(u) => imageChanged('login_background', u)}
				/>
			</div>
			<p class="hint">PNG, JPEG, WebP, GIF or SVG; up to 2 MB and 4096 px a side.</p>
		</fieldset>

		{#if editable}
			<fieldset>
				<legend>Panes</legend>
				{#if !primary}
					<p class="hint">
						Pane images are shared. Colour, dim, blur and position set here apply in {modeName(editing)} mode; the
						rest come from the {modeName(draft.base)} version.
					</p>
				{/if}
				{#each PANES as p (p)}
					{@const pd = view.panes?.[p]}
					{@const ownBg = own.panes?.[p]?.bg}
					<div class="pane">
						<div class="ph">
							<strong>{paneLabels[p]}</strong>
							<label class="pbg">
								<input
									type="color"
									value={pd?.bg ?? paneDefault(p, colors)}
									oninput={(e) => setPane(p, 'bg', e.currentTarget.value.toLowerCase())}
									aria-label="{paneLabels[p]} background colour"
								/>
								<span class="hex"
									>{pd?.bg ?? 'default'}{#if pd?.bg && !ownBg}
										· from {modeName(draft.base)}{/if}</span
								>
							</label>
							{#if ownBg}
								<button
									type="button"
									class="clear"
									aria-label="Reset the {paneLabels[p].toLowerCase()} background"
									onclick={() => setPane(p, 'bg', undefined)}>×</button
								>
							{/if}
						</div>
						<ThemeImage
							bind:listing
							slot="pane-{p}"
							label="{paneLabels[p]} image"
							hint="Drawn under a dim layer of the pane colour."
							onchange={(u) => imageChanged(`pane-${p}`, u)}
						/>
						{#if pd?.image}
							<div class="pimg">
								<label class="row">
									<span class="rl">Dim</span>
									<input
										type="range"
										min="0"
										max="1"
										step="0.05"
										value={pd.dim ?? 0.6}
										oninput={(e) => setPane(p, 'dim', Number(e.currentTarget.value))}
									/>
									<output>{Math.round((pd.dim ?? 0.6) * 100)}%</output>
								</label>
								<label class="row">
									<span class="rl">Blur</span>
									<input
										type="range"
										min="0"
										max={MAX_BLUR}
										step="1"
										value={pd.blur ?? 0}
										oninput={(e) => setPane(p, 'blur', Number(e.currentTarget.value))}
									/>
									<output>{pd.blur ?? 0} px</output>
								</label>
								<div class="row">
									<span class="rl" id="pos-{p}">Position</span>
									<Dropdown
										options={positions}
										value={pd.position ?? 'center'}
										onchange={(v: PanePosition) => setPane(p, 'position', v)}
										labelledby="pos-{p}"
										size="small"
									/>
								</div>
							</div>
						{/if}
					</div>
				{/each}
			</fieldset>
		{/if}
	</div>

	<aside class="side">
		<div class="sticky">
			<ThemePreview theme={view} name={listing.network.name} />
			{#if !editable}
				<p class="hint">How people in {modeName(editing)} mode see it now.</p>
			{/if}
			<div class="acts">
				<button class="btn btn-primary" type="button" disabled={!dirty || busy} onclick={save}
					>{busy ? 'Saving…' : 'Save theme'}</button
				>
				<button class="btn btn-ghost" type="button" disabled={!dirty || busy} onclick={discard}
					>Discard changes</button
				>
				{#if saved}
					<Confirm title="Remove the theme?" action="Remove theme" danger onconfirm={removeTheme}>
						{#snippet trigger(open)}<button class="btn btn-ghost rm" type="button" onclick={open}
								>Remove theme</button
							>{/snippet}
						Clients and this directory go back to their own look for {listing.network.name}. Uploaded theme
						images are deleted.
					</Confirm>
				{/if}
			</div>
			<p class="hint">
				{#if dirty}<strong class="unsaved">Unsaved changes.</strong>{/if}
				The theme shows on your listing here, in every directory that imports it, and in Boson when people connect.
			</p>
		</div>
	</aside>
</div>

<style>
	.editor {
		display: grid;
		grid-template-columns: minmax(0, 1fr) minmax(0, 1.25fr);
		gap: var(--space-8);
		align-items: start;
	}
	.controls {
		display: grid;
		gap: var(--space-6);
		min-width: 0;
	}
	fieldset {
		margin: 0;
		padding: 0;
		border: 0;
		display: grid;
		gap: var(--space-3);
		min-width: 0;
	}
	legend {
		padding: 0;
		margin-bottom: var(--space-1);
		font-family: var(--font-mono);
		font-size: var(--text-xs);
		font-weight: 500;
		color: var(--fg-quiet);
		letter-spacing: var(--tracking-label);
		text-transform: uppercase;
	}
	.hint {
		font-size: var(--text-sm);
		color: var(--fg-2);
	}
	.seg {
		display: inline-flex;
		width: fit-content;
		border-radius: var(--radius-sm);
		box-shadow: inset 0 0 0 1px var(--border);
		padding: 2px;
		gap: 2px;
	}
	.seg button {
		border: 0;
		background: transparent;
		color: var(--fg-2);
		font: inherit;
		font-family: var(--font-mono);
		font-size: var(--text-sm);
		padding: 4px 12px;
		border-radius: var(--radius-sm);
		cursor: pointer;
	}
	.seg button:hover {
		color: var(--fg);
	}
	.seg .tag {
		margin-left: 6px;
		font-size: var(--text-xs);
		color: var(--fg-quiet);
	}
	.variant-bar {
		display: flex;
		align-items: flex-start;
		gap: var(--space-3);
	}
	.variant-bar .hint {
		flex: 1;
	}
	.novariant {
		display: grid;
		gap: var(--space-3);
		padding: var(--space-4);
		border-radius: var(--radius-sm);
		box-shadow: inset 0 0 0 1px var(--border-soft);
		background: var(--surface);
	}
	.seg button.on {
		background: var(--surface-raised);
		color: var(--fg);
		box-shadow: inset 0 0 0 1px var(--border-strong);
	}
	.swatches {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(190px, 1fr));
		gap: var(--space-2);
	}
	.sw {
		position: relative;
		display: flex;
		align-items: center;
		border-radius: var(--radius-sm);
		box-shadow: inset 0 0 0 1px var(--border-soft);
	}
	.sw.own {
		box-shadow: inset 0 0 0 1px var(--border-strong);
		background: var(--surface);
	}
	.sw label {
		flex: 1;
		display: grid;
		grid-template-columns: auto minmax(0, 1fr);
		grid-template-rows: auto auto;
		column-gap: var(--space-2);
		align-items: center;
		padding: 6px 8px;
		cursor: pointer;
		min-width: 0;
	}
	.sw input,
	.pbg input {
		grid-row: span 2;
		width: 28px;
		height: 28px;
		padding: 0;
		border: 0;
		border-radius: var(--radius-sm);
		background: none;
		cursor: pointer;
	}
	input[type='color']::-webkit-color-swatch-wrapper {
		padding: 0;
	}
	input[type='color']::-webkit-color-swatch {
		border: 1px solid color-mix(in oklab, var(--fg), transparent 75%);
		border-radius: var(--radius-sm);
	}
	.nm {
		font-size: var(--text-sm);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.hex {
		font-family: var(--font-mono);
		font-size: var(--text-xs);
		color: var(--fg-quiet);
	}
	.own .hex {
		color: var(--fg-2);
	}
	.clear {
		flex: none;
		width: 24px;
		height: 24px;
		margin-right: 4px;
		border: 0;
		border-radius: var(--radius-sm);
		background: transparent;
		color: var(--fg-quiet);
		font-size: var(--text-lg);
		line-height: 1;
		cursor: pointer;
	}
	.clear:hover {
		color: var(--fg);
		background: var(--surface-hover);
	}
	.warn {
		display: grid;
		gap: var(--space-2);
		padding: var(--space-3) var(--space-4);
		border-radius: 0 var(--radius-sm) var(--radius-sm) 0;
		border-left: 3px solid var(--warn);
		background: color-mix(in oklab, var(--warn), transparent 90%);
		font-size: var(--text-sm);
	}
	.warn strong {
		color: var(--warn-text);
		font-family: var(--font-display);
	}
	.warn ul {
		margin: 0;
		padding-left: var(--space-4);
		font-family: var(--font-mono);
		font-size: var(--text-xs);
	}
	.warn p {
		color: var(--fg-2);
	}
	.row {
		display: grid;
		grid-template-columns: 120px minmax(0, 1fr) 52px;
		align-items: center;
		gap: var(--space-3);
	}
	.rl {
		font-size: var(--text-sm);
		color: var(--fg-2);
	}
	output {
		font-family: var(--font-mono);
		font-size: var(--text-xs);
		color: var(--fg-quiet);
		text-align: right;
	}
	input[type='range'] {
		width: 100%;
		accent-color: var(--accent);
	}
	.images {
		display: grid;
		gap: var(--space-4);
	}
	.pane {
		display: grid;
		gap: var(--space-3);
		padding: var(--space-3) var(--space-4) var(--space-4);
		border-radius: var(--radius-sm);
		box-shadow: inset 0 0 0 1px var(--border-soft);
	}
	.ph {
		display: flex;
		align-items: center;
		gap: var(--space-3);
	}
	.ph strong {
		flex: 1;
		font-family: var(--font-display);
		font-size: var(--text-sm);
	}
	.pbg {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		cursor: pointer;
	}
	.pimg {
		display: grid;
		gap: var(--space-2);
	}
	.side {
		min-width: 0;
		align-self: stretch;
	}
	.sticky {
		position: sticky;
		top: var(--space-6);
		display: grid;
		gap: var(--space-4);
	}
	.acts {
		display: flex;
		flex-wrap: wrap;
		gap: var(--space-2);
	}
	.unsaved {
		margin-right: 0.3em;
		color: var(--fg);
	}
	.rm {
		margin-left: auto;
		color: var(--danger-text);
	}
	@media (max-width: 980px) {
		.editor {
			grid-template-columns: minmax(0, 1fr);
		}
		.side {
			order: -1;
		}
		.sticky {
			position: static;
		}
	}
	@media (max-width: 480px) {
		.row {
			grid-template-columns: 96px minmax(0, 1fr) 44px;
		}
		.rm {
			margin-left: 0;
		}
	}
</style>
