import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import userEvent from '@testing-library/user-event';
import { createRawSnippet } from 'svelte';
import Confirm from './Confirm.svelte';

// jsdom has no <dialog> methods.
HTMLDialogElement.prototype.showModal ??= function (this: HTMLDialogElement) {
	this.open = true;
};
HTMLDialogElement.prototype.close ??= function (this: HTMLDialogElement) {
	this.open = false;
	this.dispatchEvent(new Event('close'));
};

const trigger = createRawSnippet((open: () => () => void) => ({
	render: () => '<button>Stop traefik</button>',
	setup: (el) => {
		el.addEventListener('click', () => open()());
	}
}));
const body = createRawSnippet(() => ({ render: () => '<p>Everything drops.</p>' }));

describe('Confirm with a typed name', () => {
	it('only confirms once the name is typed exactly', async () => {
		const onconfirm = vi.fn();
		const user = userEvent.setup();
		render(Confirm, {
			title: 'Stop traefik?',
			action: 'Stop',
			typed: 'traefik',
			onconfirm,
			trigger,
			children: body
		});
		await user.click(screen.getByText('Stop traefik'));
		const go = screen.getByRole('button', { name: 'Stop' });
		expect((go as HTMLButtonElement).disabled).toBe(true);
		const input = screen.getByRole('textbox');
		await user.type(input, 'traefi');
		expect((go as HTMLButtonElement).disabled).toBe(true);
		await user.type(input, 'k{Enter}');
		expect(onconfirm).toHaveBeenCalledOnce();
	});

	it('stays open when the action fails', async () => {
		const onconfirm = vi.fn().mockRejectedValue(new Error('nope'));
		const user = userEvent.setup();
		const { container } = render(Confirm, {
			title: 'Stop?',
			action: 'Stop',
			onconfirm,
			trigger,
			children: body
		});
		await user.click(screen.getByText('Stop traefik'));
		await user.click(screen.getByRole('button', { name: 'Stop' }));
		expect(onconfirm).toHaveBeenCalledOnce();
		expect(container.querySelector('dialog')?.open).toBe(true);
	});
});
