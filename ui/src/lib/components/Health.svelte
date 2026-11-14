<script lang="ts">
	import type { HealthStatus } from '$lib/types';
	import { num } from '$lib/format';

	let { status, users }: { status: HealthStatus; users?: number | null } = $props();
	const label: Record<HealthStatus, string> = {
		up: 'Online',
		down: 'Offline',
		unknown: 'Not checked'
	};
</script>

<span class="health" data-h={status}>
	<span class="dot" aria-hidden="true"></span>
	<span>{label[status] ?? status}</span>
	{#if status === 'up' && users !== null && users !== undefined}
		<span class="users"><strong>{num(users)}</strong> {users === 1 ? 'user' : 'users'}</span>
	{/if}
</span>

<style>
	.health {
		display: inline-flex;
		align-items: center;
		gap: var(--space-2);
		font-family: var(--font-mono);
		font-size: var(--text-xs);
		color: var(--fg-quiet);
		white-space: nowrap;
	}
	[data-h='up'] .dot {
		background: var(--success);
		box-shadow: 0 0 0 3px color-mix(in oklab, var(--success), transparent 82%);
	}
	[data-h='up'] {
		color: var(--success-text);
	}
	[data-h='down'] .dot {
		background: var(--danger);
	}
	[data-h='down'] {
		color: var(--danger-text);
	}
	.users {
		color: var(--fg-2);
	}
	.users strong {
		color: var(--fg);
		font-weight: 600;
	}
</style>
