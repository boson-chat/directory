<script lang="ts">
	import {
		PANES,
		paneDefault,
		resolveColors,
		resolveNick,
		type BosonTheme,
		type PaneName,
		type ThemeColors
	} from '$lib/theme';

	/**
	 * A small Boson client and its "Sign in to <network>" screen painted with
	 * a theme, as the client would (unguarded: the editor shows contrast
	 * warnings next to it).
	 */
	let { theme, name }: { theme: BosonTheme; name: string } = $props();

	let c = $derived<ThemeColors>(resolveColors(theme));
	let nick = $derived(resolveNick(theme));
	let radius = $derived(theme.radius ?? 2);
	let initial = $derived((name.trim()[0] ?? '#').toUpperCase());

	let vars = $derived(
		[
			...Object.entries(c).map(([k, v]) => `--t-${k}: ${v}`),
			`--t-r: ${radius}px`,
			`--t-body: ${theme.font === 'system' ? 'system-ui, -apple-system, sans-serif' : 'var(--font-body)'}`,
			`--t-head: ${theme.font === 'system' ? 'system-ui, -apple-system, sans-serif' : 'var(--font-display)'}`,
			`color-scheme: ${theme.base}`
		].join('; ')
	);

	function paneStyle(p: PaneName): string {
		const pane = theme.panes?.[p];
		return `--pane-bg: ${pane?.bg ?? paneDefault(p, c)}`;
	}
	function imageStyle(p: PaneName): string {
		const pane = theme.panes?.[p];
		if (!pane?.image) return '';
		const blur = pane.blur ?? 0;
		return [
			`background-image: url("${pane.image.replace(/"/g, '%22')}")`,
			`background-position: ${pane.position ?? 'center'}`,
			blur ? `filter: blur(${blur / 2}px)` : '',
			blur ? 'transform: scale(1.08)' : ''
		]
			.filter(Boolean)
			.join('; ');
	}
	const dimOf = (p: PaneName) => theme.panes?.[p]?.dim ?? 0.6;

	const nickColor = (n: string) => {
		let h = 0;
		for (const ch of n) h = (h * 31 + ch.charCodeAt(0)) % 360;
		return `hsl(${h} ${nick.saturation}% ${nick.lightness}%)`;
	};
	const messages = [
		{ nick: 'ada', text: 'morning all, the new build is up' },
		{ nick: 'grace', text: 'nice, release notes in #announcements?' },
		{ nick: 'linus', text: 'pinned them a minute ago' },
		{ nick: 'ada', text: 'thanks! welcome to the newcomers 👋' }
	];
	const channels = ['general', 'help', 'announcements', 'offtopic'];
	const members = ['ada', 'grace', 'linus', 'margaret', 'ken'];
</script>

<div class="previews" style={vars}>
	<figure class="client" aria-label="Preview of the Boson client with this theme">
		{#each PANES as p (p)}
			<div class="pane {p}" style={paneStyle(p)}>
				{#if theme.panes?.[p]?.image}
					<div class="img" style={imageStyle(p)}></div>
					<div class="dim" style="opacity: {dimOf(p)}"></div>
				{/if}
				<div class="content">
					{#if p === 'servers'}
						<span class="tile active">
							{#if theme.logo}<img src={theme.logo} alt="" />{:else}{initial}{/if}
						</span>
						<span class="tile"></span>
						<span class="tile"></span>
						<span class="tile add">+</span>
					{:else if p === 'channels'}
						<div class="net">
							{#if theme.logo_wide}
								<img class="wide" src={theme.logo_wide} alt="" />
							{:else}
								<strong>{name}</strong>
							{/if}
						</div>
						{#each channels as ch, i (ch)}
							<span class="ch" class:sel={i === 0}><span class="hash">#</span>{ch}</span>
						{/each}
					{:else if p === 'messages'}
						<div class="topic"><strong>#general</strong><span>Say hi</span></div>
						<div class="msgs">
							{#each messages as m, i (i)}
								<p><b style="color: {nickColor(m.nick)}">{m.nick}</b> {m.text}</p>
							{/each}
						</div>
						<div class="composer">Message #general</div>
					{:else}
						<span class="mh">Online · {members.length}</span>
						{#each members as m (m)}
							<span class="mem"><i style="background: {nickColor(m)}"></i>{m}</span>
						{/each}
					{/if}
				</div>
			</div>
		{/each}
		<figcaption>Client</figcaption>
	</figure>

	<figure class="login" aria-label="Preview of the sign-in screen with this theme">
		{#if theme.login_background}
			<div
				class="lbg"
				style="background-image: url(&quot;{theme.login_background.replace(/"/g, '%22')}&quot;)"
			></div>
			<div class="ldim"></div>
		{/if}
		<div class="card">
			<span class="mark">
				{#if theme.logo}<img src={theme.logo} alt="" />{:else}{initial}{/if}
			</span>
			<strong class="lt">Sign in to {name}</strong>
			<span class="in">Username</span>
			<span class="in">Password</span>
			<span class="go">Sign in</span>
			<span class="alt">Create an account</span>
		</div>
		<figcaption>Sign-in screen</figcaption>
	</figure>
</div>

<style>
	.previews {
		display: grid;
		grid-template-columns: minmax(0, 1.9fr) minmax(0, 1fr);
		gap: var(--space-3);
		font-family: var(--t-body);
	}
	figure {
		margin: 0;
		position: relative;
		border-radius: var(--radius-md);
		overflow: hidden;
		box-shadow:
			0 0 0 1px var(--border),
			var(--shadow);
		height: 280px;
		color: var(--t-fg);
		background: var(--t-bg);
		font-size: 10px;
		line-height: 1.35;
	}
	figcaption {
		position: absolute;
		right: 6px;
		bottom: 6px;
		padding: 1px 6px;
		border-radius: 2px;
		font-family: var(--font-mono);
		font-size: 9px;
		letter-spacing: var(--tracking-label);
		text-transform: uppercase;
		color: #e2e2ea;
		background: rgba(7, 7, 9, 0.72);
		z-index: 3;
	}
	.client {
		display: grid;
		grid-template-columns: 38px minmax(0, 0.9fr) minmax(0, 2.2fr) minmax(0, 0.8fr);
	}
	.pane {
		position: relative;
		background: var(--pane-bg);
		min-width: 0;
		overflow: hidden;
	}
	.pane + .pane {
		box-shadow: inset 1px 0 0 var(--t-border-soft);
	}
	.img,
	.dim,
	.lbg,
	.ldim {
		position: absolute;
		inset: 0;
	}
	.img,
	.lbg {
		background-size: cover;
	}
	.dim {
		background: var(--pane-bg);
	}
	.content {
		position: relative;
		height: 100%;
		display: flex;
		flex-direction: column;
		gap: 4px;
		padding: 8px 7px;
		min-width: 0;
	}
	.servers .content {
		align-items: center;
		gap: 6px;
		padding: 8px 0;
	}
	.tile {
		width: 24px;
		height: 24px;
		border-radius: calc(var(--t-r) * 2);
		background: var(--t-surface-raised);
		box-shadow: inset 0 0 0 1px var(--t-border);
		display: grid;
		place-items: center;
		overflow: hidden;
		font-family: var(--t-head);
		font-weight: 700;
		color: var(--t-fg-2);
	}
	.tile.active {
		background: var(--t-accent);
		color: var(--t-accent-on);
		box-shadow:
			0 0 0 2px var(--t-bg),
			0 0 0 3px var(--t-accent);
	}
	.tile img,
	.mark img {
		width: 100%;
		height: 100%;
		object-fit: cover;
	}
	.tile.add {
		background: transparent;
		color: var(--t-success);
	}
	.net {
		height: 22px;
		display: flex;
		align-items: center;
		margin-bottom: 4px;
		padding-bottom: 6px;
		box-shadow: 0 1px 0 var(--t-border-soft);
		font-family: var(--t-head);
		font-size: 11px;
		overflow: hidden;
		white-space: nowrap;
		text-overflow: ellipsis;
	}
	.net img.wide {
		max-height: 18px;
		max-width: 100%;
		object-fit: contain;
	}
	.ch {
		padding: 2px 5px;
		border-radius: var(--t-r);
		color: var(--t-fg-2);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}
	.ch.sel {
		background: var(--t-surface-hover);
		color: var(--t-fg);
	}
	.hash {
		color: var(--t-fg-3);
		margin-right: 2px;
	}
	.topic {
		display: flex;
		gap: 6px;
		align-items: baseline;
		padding-bottom: 6px;
		box-shadow: 0 1px 0 var(--t-border-soft);
		font-family: var(--t-head);
		font-size: 11px;
	}
	.topic span {
		color: var(--t-fg-3);
		font-family: var(--t-body);
		font-size: 9px;
	}
	.msgs {
		flex: 1;
		display: flex;
		flex-direction: column;
		justify-content: flex-end;
		gap: 5px;
	}
	.msgs p {
		margin: 0;
		color: var(--t-fg);
	}
	.composer {
		padding: 5px 7px;
		border-radius: var(--t-r);
		background: var(--t-surface);
		box-shadow: inset 0 0 0 1px var(--t-border);
		color: var(--t-fg-3);
	}
	.mh {
		font-size: 8px;
		letter-spacing: var(--tracking-label);
		text-transform: uppercase;
		color: var(--t-fg-3);
		margin-bottom: 2px;
	}
	.mem {
		display: flex;
		align-items: center;
		gap: 5px;
		color: var(--t-fg-2);
		white-space: nowrap;
		overflow: hidden;
	}
	.mem i {
		width: 10px;
		height: 10px;
		flex: none;
		border-radius: 50%;
	}
	.login {
		display: grid;
		place-items: center;
	}
	.ldim {
		background: var(--t-bg);
		opacity: 0.35;
	}
	.card {
		position: relative;
		z-index: 1;
		width: min(78%, 190px);
		display: grid;
		gap: 6px;
		justify-items: center;
		padding: 14px 12px;
		border-radius: calc(var(--t-r) * 3);
		background: var(--t-surface);
		box-shadow:
			0 0 0 1px var(--t-border),
			0 10px 30px rgba(0, 0, 0, 0.35);
	}
	.mark {
		width: 30px;
		height: 30px;
		border-radius: calc(var(--t-r) * 2);
		overflow: hidden;
		display: grid;
		place-items: center;
		background: var(--t-accent);
		color: var(--t-accent-on);
		font-family: var(--t-head);
		font-weight: 700;
		font-size: 14px;
	}
	.lt {
		font-family: var(--t-head);
		font-size: 11px;
		text-align: center;
	}
	.in,
	.go {
		width: 100%;
		padding: 5px 7px;
		border-radius: var(--t-r);
	}
	.in {
		background: var(--t-bg);
		box-shadow: inset 0 0 0 1px var(--t-border);
		color: var(--t-fg-3);
	}
	.go {
		text-align: center;
		font-weight: 600;
		background: var(--t-accent);
		color: var(--t-accent-on);
	}
	.alt {
		color: var(--t-fg-2);
		font-size: 9px;
	}
	@media (max-width: 720px) {
		.previews {
			grid-template-columns: minmax(0, 1fr);
		}
		.client {
			grid-template-columns: 34px minmax(0, 0.9fr) minmax(0, 2fr) minmax(0, 0.8fr);
		}
		figure {
			height: 240px;
		}
	}
</style>
