<script lang="ts">
	import { goto } from '$app/navigation';
	import { directory } from '$lib/api';
	import type { NetworkInput } from '$lib/types';
	import { fieldErrors } from '$lib/errors';
	import { toasts, errorText } from '$lib/toasts.svelte';
	import NetworkForm from '$lib/components/NetworkForm.svelte';

	async function create(v: NetworkInput) {
		try {
			const { network: n } = await directory.create(v);
			toasts.push(`${n.name} submitted. Add the DNS record next.`);
			await goto(`/me/${encodeURIComponent(n.id)}`);
		} catch (e) {
			const f = fieldErrors(e);
			if (f) return f;
			toasts.push(errorText(e), 'bad');
		}
	}
</script>

<svelte:head><title>List a network · boson directory</title></svelte:head>

<p class="back"><a href="/me">My networks</a></p>
<div class="page-head">
	<div>
		<h1>List a network</h1>
		<p>
			Describe the network, then prove you run its hostname with a DNS TXT record. It shows up in the
			directory once three public resolvers see the record.
		</p>
	</div>
</div>

<NetworkForm
	mode="create"
	submitLabel="Submit and get the DNS record"
	onsubmit={create}
	oncancel={() => goto('/me')}
/>
