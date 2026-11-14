<script lang="ts">
	import type { Snippet } from 'svelte';
	import { page } from '$app/state';
	import { goto } from '$app/navigation';
	import { auth } from '$lib/auth.svelte';
	import { toasts } from '$lib/toasts.svelte';
	import Logo from './Logo.svelte';
	import Dropdown from './Dropdown.svelte';
	import type { DropdownOption } from '$lib/dropdown';

	let { children }: { children: Snippet } = $props();

	let nav = $derived([
		{ href: '/', label: 'Browse' },
		{ href: '/me', label: 'My networks' },
		...(auth.can('directory.manage') ? [{ href: '/operator', label: 'Operator' }] : [])
	]);

	let menuOpen = $state(false);
	type Theme = 'system' | 'dark' | 'light';
	let theme = $state<Theme>('system');
	const themes: DropdownOption<Theme>[] = [
		{ value: 'system', label: 'System', description: 'Follow the device setting' },
		{ value: 'dark', label: 'Dark', description: 'Terminal' },
		{ value: 'light', label: 'Light', description: 'Daylight' }
	];

	$effect(() => {
		try {
			const t = localStorage.getItem('theme');
			if (t === 'dark' || t === 'light') theme = t;
		} catch {
			/* storage blocked */
		}
	});
	$effect(() => {
		if (theme === 'system') document.documentElement.removeAttribute('data-theme');
		else document.documentElement.dataset.theme = theme;
	});
	function saveTheme() {
		try {
			if (theme === 'system') localStorage.removeItem('theme');
			else localStorage.setItem('theme', theme);
		} catch {
			/* storage blocked */
		}
	}

	function active(href: string) {
		const p = page.url.pathname;
		if (href === '/') return p === '/' || p.startsWith('/n/');
		return p === href || p.startsWith(href + '/');
	}

	let signinHref = $derived(
		'/signin' +
			(page.url.pathname !== '/' && page.url.pathname !== '/signin'
				? '?next=' + encodeURIComponent(page.url.pathname + page.url.search)
				: '')
	);

	async function signOut() {
		await auth.logout();
		toasts.push('You signed out.');
		if (page.url.pathname.startsWith('/me') || page.url.pathname.startsWith('/operator')) goto('/');
	}

	$effect(() => {
		void page.url.pathname;
		menuOpen = false;
	});
</script>

<a class="skip" href="#main">Skip to content</a>
<header class="bar">
	<div class="inner">
		<a class="brand" href="/" aria-label="boson directory, browse networks">
			<span class="mark"><Logo size={20} /></span>
			<span class="word">Boson</span>
			<span class="sub">directory</span>
		</a>
		<button
			class="menu btn btn-small"
			aria-expanded={menuOpen}
			aria-controls="nav"
			onclick={() => (menuOpen = !menuOpen)}>{menuOpen ? 'Close' : 'Menu'}</button
		>
		<nav id="nav" class:open={menuOpen} aria-label="Main">
			<ul>
				{#each nav as item (item.href)}
					<li>
						<a href={item.href} aria-current={active(item.href) ? 'page' : undefined}>{item.label}</a>
					</li>
				{/each}
			</ul>
			<div class="side">
				<div class="theme">
					<span class="sr-only" id="theme-label">Theme</span>
					<Dropdown
						options={themes}
						bind:value={theme}
						onchange={saveTheme}
						labelledby="theme-label"
						size="small"
						align="end"
						title="Theme"
					/>
				</div>
				{#if auth.signedIn}
					<span class="who" title="Signed in with your boson account">{auth.username}</span>
					<button class="btn btn-small btn-ghost" onclick={signOut}>Sign out</button>
				{:else if auth.ready}
					<a class="btn btn-small" href={signinHref}>Sign in</a>
				{/if}
			</div>
		</nav>
	</div>
</header>
<main id="main" tabindex="-1">
	{@render children()}
</main>
<footer class="foot">
	<div class="inner">
		<span>Listings are checked against each network’s own DNS record.</span>
		<a href="/.well-known/boson-directory.json">Directory info</a>
	</div>
</footer>

<style>
	.skip {
		position: absolute;
		left: -9999px;
	}
	.skip:focus {
		left: var(--space-4);
		top: var(--space-4);
		z-index: 100;
		background: var(--surface-raised);
		color: var(--fg);
		font-family: var(--font-mono);
		font-size: var(--text-sm);
		padding: var(--space-2) var(--space-3);
	}
	.bar {
		position: sticky;
		top: 0;
		z-index: 20;
		background: color-mix(in oklab, var(--surface) 92%, transparent);
		backdrop-filter: blur(12px);
		border-bottom: 1px solid var(--border);
	}
	.inner {
		max-width: var(--page);
		margin: 0 auto;
		padding: 0 clamp(16px, 4vw, 40px);
		display: flex;
		align-items: center;
		gap: var(--space-6);
	}
	.bar .inner {
		min-height: 56px;
	}
	.brand {
		display: flex;
		align-items: center;
		gap: var(--space-2);
		color: var(--fg);
		text-decoration: none;
		flex: none;
	}
	.brand:hover {
		color: var(--fg);
	}
	.mark {
		display: inline-flex;
		color: var(--accent);
	}
	.word {
		font-family: var(--font-display);
		font-size: 17px;
		font-weight: 600;
		letter-spacing: var(--tracking-display);
	}
	.sub {
		font-family: var(--font-mono);
		font-size: var(--text-sm);
		color: var(--fg-quiet);
	}
	.menu {
		display: none;
		margin-left: auto;
	}
	nav {
		display: flex;
		align-items: center;
		gap: var(--space-6);
		flex: 1;
		min-width: 0;
	}
	nav ul {
		list-style: none;
		margin: 0;
		padding: 0;
		display: flex;
		gap: var(--space-1);
	}
	nav ul a {
		display: block;
		padding: var(--space-2) var(--space-3);
		border-radius: var(--radius-sm);
		color: var(--fg-2);
		text-decoration: none;
		font-family: var(--font-display);
		font-size: var(--text-sm);
		font-weight: 600;
		letter-spacing: var(--tracking-display);
		position: relative;
		transition:
			background-color var(--motion-fast) var(--ease-standard),
			color var(--motion-fast) var(--ease-standard);
	}
	nav ul a:hover {
		color: var(--fg);
		background: var(--surface-hover);
	}
	nav ul a[aria-current='page'] {
		color: var(--accent-text);
	}
	nav ul a[aria-current='page']::after {
		content: '';
		position: absolute;
		left: var(--space-3);
		right: var(--space-3);
		bottom: -11px;
		height: 2px;
		background: var(--accent);
	}
	.side {
		margin-left: auto;
		display: flex;
		align-items: center;
		gap: var(--space-3);
	}
	.theme {
		width: 112px;
	}
	.who {
		font-family: var(--font-mono);
		font-size: var(--text-sm);
		font-weight: 600;
		color: var(--fg);
		max-width: 16ch;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	main {
		max-width: var(--page);
		margin: 0 auto;
		padding: var(--space-8) clamp(16px, 4vw, 40px) var(--space-16);
		min-height: calc(100vh - 56px - 64px);
		outline: none;
		box-shadow: none;
	}
	.foot {
		border-top: 1px solid var(--border-soft);
		font-family: var(--font-mono);
		font-size: var(--text-xs);
		color: var(--fg-quiet);
	}
	.foot .inner {
		min-height: 64px;
		justify-content: space-between;
		flex-wrap: wrap;
		gap: var(--space-2) var(--space-6);
		padding-top: var(--space-4);
		padding-bottom: var(--space-4);
	}
	.foot a {
		color: var(--fg-quiet);
	}
	.foot a:hover {
		color: var(--fg);
	}

	@media (max-width: 760px) {
		.bar .inner {
			flex-wrap: wrap;
			gap: 0;
		}
		.menu {
			display: inline-flex;
		}
		nav {
			display: none;
			flex-basis: 100%;
			flex-direction: column;
			align-items: stretch;
			gap: var(--space-3);
			padding: var(--space-2) 0 var(--space-4);
		}
		nav.open {
			display: flex;
		}
		nav ul {
			flex-direction: column;
		}
		nav ul a[aria-current='page'] {
			background: var(--surface-raised);
			box-shadow: inset 2px 0 0 var(--accent);
		}
		nav ul a[aria-current='page']::after {
			display: none;
		}
		.side {
			margin-left: 0;
			flex-wrap: wrap;
			padding-top: var(--space-3);
			border-top: 1px solid var(--border-soft);
		}
		main {
			padding-top: var(--space-6);
		}
	}
</style>
