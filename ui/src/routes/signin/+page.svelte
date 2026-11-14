<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import { auth, authErrorText, safeNext } from '$lib/auth.svelte';
	import { config } from '$lib/config';
	import type { SsoProvider } from '$lib/types';
	import Logo from '$lib/components/Logo.svelte';
	import Notice from '$lib/components/Notice.svelte';

	let login = $state('');
	let password = $state('');
	let busy = $state(false);
	let error = $state(page.url.searchParams.get('error') ?? '');
	let providers = $state<SsoProvider[]>([]);
	let authHost = $state('');
	let next = $derived(safeNext(page.url.searchParams.get('next')));

	$effect(() => {
		if (auth.signedIn) goto(next, { replaceState: true });
	});
	$effect(() => {
		config().then((c) => (authHost = new URL(c.auth_url).host));
		auth
			.providers()
			.then((c) => (providers = c.sso_providers ?? []))
			.catch(() => (providers = []));
	});

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		busy = true;
		error = '';
		try {
			await auth.login(login.trim(), password);
			password = '';
		} catch (err) {
			error = authErrorText(err);
		} finally {
			busy = false;
		}
	}

	async function sso(p: SsoProvider) {
		error = '';
		try {
			await auth.startSso(p.start_url, next);
		} catch (err) {
			error = authErrorText(err);
		}
	}
</script>

<svelte:head><title>Sign in · boson directory</title></svelte:head>

<div class="wrap">
	<div class="col">
		<div class="logo">
			<span class="mark"><Logo size={26} /></span>
			<h1>Sign in to list your network</h1>
			<p class="lede">Use your boson account. Browsing needs no account.</p>
		</div>
		<form class="card" onsubmit={submit} aria-describedby={error ? 'login-error' : undefined}>
			{#if error}
				<div id="login-error"><Notice>{error}</Notice></div>
			{/if}
			<label class="field">
				<span>Username or email</span>
				<input
					type="text"
					bind:value={login}
					autocomplete="username"
					required
					autocapitalize="none"
					spellcheck="false"
				/>
			</label>
			<label class="field">
				<span>Password</span>
				<input type="password" bind:value={password} autocomplete="current-password" required />
			</label>
			<button class="btn btn-primary" type="submit" disabled={busy}>{busy ? 'Signing in…' : 'Sign in'}</button
			>
			{#if providers.length}
				<div class="or" aria-hidden="true"><span>or</span></div>
				<div class="sso">
					{#each providers as p (p.slug)}
						<button class="btn" type="button" onclick={() => sso(p)}>Continue with {p.display_name}</button>
					{/each}
				</div>
			{/if}
		</form>
		<p class="help">
			Your password goes to {authHost || 'the boson sign-in service'}, not to this directory. No account yet?
			Create one in the boson app.
		</p>
	</div>
</div>

<style>
	.wrap {
		display: grid;
		place-items: center;
		padding: var(--space-8) 0;
	}
	.col {
		width: min(400px, 100%);
		display: grid;
		gap: var(--space-6);
	}
	.logo {
		text-align: center;
		display: grid;
		justify-items: center;
		gap: var(--space-2);
	}
	.mark {
		color: var(--accent);
		display: inline-flex;
		margin-bottom: var(--space-2);
	}
	h1 {
		font-size: var(--text-xl);
	}
	.lede {
		color: var(--fg-2);
		font-size: var(--text-sm);
	}
	.card {
		display: grid;
		gap: var(--space-5);
		padding: var(--space-6);
		background: var(--surface);
		border: 1px solid var(--border);
		border-radius: var(--radius-sm);
	}
	input {
		min-height: 44px;
		padding: var(--space-3) var(--space-4);
	}
	.card > .btn,
	.sso .btn {
		width: 100%;
		min-height: 40px;
	}
	.or {
		display: flex;
		align-items: center;
		gap: var(--space-3);
		font-family: var(--font-mono);
		font-size: var(--text-xs);
		color: var(--fg-quiet);
	}
	.or::before,
	.or::after {
		content: '';
		flex: 1;
		height: 1px;
		background: var(--border-soft);
	}
	.sso {
		display: grid;
		gap: var(--space-2);
		margin-top: calc(-1 * var(--space-2));
	}
	.help {
		font-family: var(--font-mono);
		font-size: var(--text-xs);
		line-height: 1.6;
		color: var(--fg-quiet);
		text-align: center;
		padding: 0 var(--space-2);
	}
	@media (max-width: 480px) {
		.card {
			padding: var(--space-5) var(--space-4);
		}
	}
</style>
