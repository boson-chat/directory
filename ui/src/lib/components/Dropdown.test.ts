import { describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/svelte';
import userEvent from '@testing-library/user-event';
import Dropdown from './Dropdown.svelte';
import type { DropdownOption } from '$lib/dropdown';

const options: DropdownOption[] = [
	{ value: '1h', label: 'Last hour', group: 'Recent' },
	{ value: '24h', label: 'Last 24 hours', group: 'Recent' },
	{ value: 'x', label: 'Locked', disabled: true, group: 'Recent' },
	{ value: '168h', label: 'Last 7 days', description: 'A week', group: 'Older' },
	{ value: '', label: 'Any time', group: 'Older' }
];

function setup(props: Record<string, unknown> = {}) {
	const onchange = vi.fn();
	const user = userEvent.setup();
	const r = render(Dropdown, { options, value: '24h', label: 'When', onchange, ...props });
	const combo = screen.getByRole('combobox', { name: 'When' });
	return { ...r, user, onchange, combo };
}

const activeLabel = (combo: HTMLElement) =>
	document.getElementById(combo.getAttribute('aria-activedescendant') ?? '')?.querySelector('.opt-label')
		?.textContent;

describe('Dropdown', () => {
	it('renders a closed select-only combobox showing the value', () => {
		const { combo } = setup();
		expect(combo).toHaveProperty('textContent', expect.stringContaining('Last 24 hours'));
		expect(combo.getAttribute('aria-expanded')).toBe('false');
		expect(combo.getAttribute('aria-haspopup')).toBe('listbox');
		expect(screen.queryByRole('listbox')).toBeNull(); // hidden
	});

	it('opens on click with the selected option active, and chooses by click', async () => {
		const { combo, user, onchange } = setup();
		await user.click(combo);
		expect(combo.getAttribute('aria-expanded')).toBe('true');
		const list = screen.getByRole('listbox');
		expect(combo.getAttribute('aria-controls')).toBe(list.id);
		expect(activeLabel(combo)).toBe('Last 24 hours');
		const sel = screen.getByRole('option', { selected: true });
		expect(sel.textContent).toContain('Last 24 hours');
		expect(screen.getAllByRole('group').map((g) => g.getAttribute('aria-labelledby'))).toHaveLength(2);

		await user.click(screen.getByRole('option', { name: 'Last 7 days' }));
		expect(onchange).toHaveBeenCalledWith('168h');
		expect(combo.getAttribute('aria-expanded')).toBe('false');
		expect(document.activeElement).toBe(combo);
		expect(combo.textContent).toContain('Last 7 days');
	});

	it('supports the keyboard: arrows skip disabled, Home/End, Enter chooses', async () => {
		const { combo, user, onchange } = setup();
		combo.focus();
		await user.keyboard('{ArrowDown}');
		expect(combo.getAttribute('aria-expanded')).toBe('true');
		expect(activeLabel(combo)).toBe('Last 24 hours');
		await user.keyboard('{ArrowDown}');
		expect(activeLabel(combo)).toBe('Last 7 days'); // skipped "Locked"
		await user.keyboard('{End}');
		expect(activeLabel(combo)).toBe('Any time');
		await user.keyboard('{Home}');
		expect(activeLabel(combo)).toBe('Last hour');
		await user.keyboard('{ArrowUp}');
		expect(activeLabel(combo)).toBe('Last hour');
		await user.keyboard('{Enter}');
		expect(onchange).toHaveBeenCalledWith('1h');
		expect(combo.getAttribute('aria-expanded')).toBe('false');
		expect(document.activeElement).toBe(combo);
	});

	it('Space opens and chooses; Escape closes without changing', async () => {
		const { combo, user, onchange } = setup();
		combo.focus();
		await user.keyboard(' ');
		expect(combo.getAttribute('aria-expanded')).toBe('true');
		await user.keyboard('{ArrowDown}{Escape}');
		expect(combo.getAttribute('aria-expanded')).toBe('false');
		expect(onchange).not.toHaveBeenCalled();
		expect(combo.textContent).toContain('Last 24 hours');
		await user.keyboard(' {ArrowUp} ');
		expect(onchange).toHaveBeenCalledWith('1h');
	});

	it('type-ahead opens and jumps to a match; disabled options are not chosen', async () => {
		const { combo, user } = setup();
		combo.focus();
		await user.keyboard('a');
		expect(combo.getAttribute('aria-expanded')).toBe('true');
		expect(activeLabel(combo)).toBe('Any time');
		await user.keyboard('{Escape}');
		await user.click(combo);
		await user.click(screen.getByRole('option', { name: 'Locked' }));
		expect(combo.getAttribute('aria-expanded')).toBe('true');
		expect(screen.getByRole('option', { name: 'Locked' }).getAttribute('aria-disabled')).toBe('true');
	});

	it('Tab chooses the active option and closes', async () => {
		const { combo, user, onchange } = setup();
		combo.focus();
		await user.keyboard('{ArrowDown}{ArrowDown}');
		await user.tab();
		expect(onchange).toHaveBeenCalledWith('168h');
		expect(combo.getAttribute('aria-expanded')).toBe('false');
	});

	it('closes on a click outside', async () => {
		const { combo, user } = setup();
		await user.click(combo);
		await user.click(document.body);
		expect(combo.getAttribute('aria-expanded')).toBe('false');
	});

	it('links option descriptions and submits through a hidden input', async () => {
		const { combo, user, container } = setup({ name: 'since' });
		const hidden = container.querySelector<HTMLInputElement>('input[type=hidden][name=since]');
		expect(hidden?.value).toBe('24h');
		await user.click(combo);
		const week = screen.getByRole('option', { name: 'Last 7 days' });
		expect(document.getElementById(week.getAttribute('aria-describedby') ?? '')?.textContent).toBe('A week');
		await user.click(week);
		expect(hidden?.value).toBe('168h');
	});

	it('does nothing when disabled', async () => {
		const { combo, user } = setup({ disabled: true });
		expect(combo).toHaveProperty('disabled', true);
		await user.click(combo);
		expect(combo.getAttribute('aria-expanded')).toBe('false');
	});

	it('shows the placeholder when the value matches no option', () => {
		const { combo } = setup({ value: 'nope', placeholder: 'Pick one' });
		expect(combo.textContent).toContain('Pick one');
	});
});

describe('Dropdown (multiple)', () => {
	const roles: DropdownOption[] = [
		{ value: 'admin', label: 'Admin', disabled: true, description: 'Above your role' },
		{ value: 'mod', label: 'Moderator' },
		{ value: 'dj', label: 'DJ' }
	];
	function multi(values: string[]) {
		const onchangemany = vi.fn();
		const user = userEvent.setup();
		render(Dropdown, {
			options: roles,
			multiple: true,
			values,
			label: 'Roles',
			onchangemany,
			placeholder: 'Add roles'
		});
		return { user, onchangemany, combo: screen.getByRole('combobox', { name: 'Roles' }) };
	}

	it('toggles several options and reports once on close', async () => {
		const { user, onchangemany, combo } = multi(['admin']);
		expect(combo.textContent).toContain('Admin');
		await user.click(combo);
		const list = screen.getByRole('listbox');
		expect(list.getAttribute('aria-multiselectable')).toBe('true');
		await user.click(screen.getByRole('option', { name: 'Moderator' }));
		await user.click(screen.getByRole('option', { name: 'DJ' }));
		expect(combo.getAttribute('aria-expanded')).toBe('true'); // stays open
		expect(screen.getAllByRole('option', { selected: true })).toHaveLength(3);
		expect(onchangemany).not.toHaveBeenCalled();
		await user.keyboard('{Escape}');
		expect(onchangemany).toHaveBeenCalledTimes(1);
		expect(onchangemany).toHaveBeenCalledWith(['admin', 'mod', 'dj']);
		expect(combo.textContent).toContain('Admin, Moderator, DJ');
	});

	it('keeps disabled options as they are and toggles with the keyboard', async () => {
		const { user, onchangemany, combo } = multi(['admin', 'dj']);
		combo.focus();
		await user.keyboard('{ArrowDown}'); // opens on the first chosen enabled… or first enabled
		await user.keyboard('{Home}{Enter}'); // Home skips the disabled Admin → Moderator on
		await user.keyboard('{End}{ }'); // DJ off
		await user.keyboard('{ArrowUp}{ArrowUp}{Enter}'); // can't reach Admin
		await user.tab();
		expect(onchangemany).toHaveBeenCalledWith(['admin']);
	});

	it('reports nothing when nothing changed', async () => {
		const { user, onchangemany, combo } = multi([]);
		expect(combo.textContent).toContain('Add roles');
		await user.click(combo);
		await user.click(screen.getByRole('option', { name: 'DJ' }));
		await user.click(screen.getByRole('option', { name: 'DJ' }));
		await user.click(document.body);
		expect(onchangemany).not.toHaveBeenCalled();
	});
});
