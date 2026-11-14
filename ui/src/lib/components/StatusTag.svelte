<script lang="ts">
	import type { NetworkStatus } from '$lib/types';
	let { status }: { status: NetworkStatus } = $props();
	const text: Record<NetworkStatus, string> = {
		verified: 'Verified',
		pending: 'Pending DNS',
		lapsed: 'Lapsed',
		blocked: 'Blocked'
	};
	const tone: Record<NetworkStatus, string> = {
		verified: 'tag-ok',
		pending: 'tag-warn',
		lapsed: 'tag-bad',
		blocked: 'tag-bad'
	};
</script>

<span class="tag {tone[status] ?? ''}"
	><span class="dot" aria-hidden="true"></span>{text[status] ?? status}</span
>

<style>
	.dot {
		background: currentColor;
	}
</style>
