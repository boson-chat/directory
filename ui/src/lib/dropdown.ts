/** One choice in a <Dropdown>. */
export interface DropdownOption<V extends string = string> {
	value: V;
	label: string;
	/** A second, quieter line under the label. */
	description?: string;
	disabled?: boolean;
	/** Options with the same group (consecutive) are shown under one heading. */
	group?: string;
}

/**
 * The next enabled option from `from` in direction `dir`, without wrapping.
 * Returns `from` when there is none (so the highlight stays put at the ends).
 */
export function step(options: DropdownOption[], from: number, dir: 1 | -1, by = 1): number {
	let found = from;
	let i = from;
	let moved = 0;
	while (moved < by) {
		i += dir;
		if (i < 0 || i >= options.length) break;
		if (!options[i].disabled) {
			found = i;
			moved++;
		}
	}
	return found;
}

/** The first (dir 1) or last (dir -1) enabled option, or -1. */
export function edge(options: DropdownOption[], dir: 1 | -1): number {
	if (dir === 1) return options.findIndex((o) => !o.disabled);
	for (let i = options.length - 1; i >= 0; i--) if (!options[i].disabled) return i;
	return -1;
}

/**
 * Type-ahead: the next enabled option whose label starts with `query`,
 * searching after `from` and wrapping. Typing one letter repeatedly
 * ("a", "aa", …) cycles through the options starting with that letter.
 */
export function match(options: DropdownOption[], query: string, from: number): number {
	const q = query.toLocaleLowerCase();
	if (!q) return -1;
	const same = [...q].every((c) => c === q[0]);
	const needle = same ? q[0] : q;
	// A longer query keeps the current option if it still matches.
	const start = same ? from + 1 : Math.max(from, 0);
	const n = options.length;
	for (let k = 0; k < n; k++) {
		const i = (start + k + n) % n;
		const o = options[i];
		if (!o.disabled && o.label.toLocaleLowerCase().startsWith(needle)) return i;
	}
	return -1;
}

export interface Placement {
	/** Open upward: not enough room below and more room above. */
	up: boolean;
	/** Align to the trigger's right edge (preferred, or aligning left would overflow). */
	end: boolean;
	/** Max height for the list, in px. */
	maxHeight: number;
	/** Max width for the list, in px, so it never runs off-screen. */
	maxWidth: number;
}

const GAP = 6; // between trigger and list
const MARGIN = 8; // from the viewport edge
const MAX = 320;

/** Where to put the list given the trigger's rect, the list's natural size and the viewport. */
export function place(
	trigger: { top: number; bottom: number; left: number; right: number },
	list: { width: number; height: number },
	viewport: { width: number; height: number },
	prefer: 'start' | 'end' = 'start'
): Placement {
	const below = viewport.height - trigger.bottom - GAP - MARGIN;
	const above = trigger.top - GAP - MARGIN;
	const want = Math.min(list.height, MAX);
	const up = below < want && above > below;
	const room = Math.max(up ? above : below, 120);
	const roomRight = viewport.width - trigger.left - MARGIN; // aligned to the trigger's left edge
	const roomLeft = trigger.right - MARGIN; // aligned to its right edge
	const end =
		prefer === 'end'
			? list.width <= roomLeft || roomLeft >= roomRight
			: list.width > roomRight && roomLeft > roomRight;
	return {
		up,
		end,
		maxHeight: Math.floor(Math.min(MAX, room)),
		maxWidth: Math.floor(Math.max(end ? roomLeft : roomRight, 160))
	};
}
