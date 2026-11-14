/** True when a drag carries files (not text, links or page elements). */
export function hasFiles(e: DragEvent): boolean {
	return !!e.dataTransfer && Array.from(e.dataTransfer.types).includes('Files');
}

/**
 * Makes an element a drop target for one file. `over` is true while files
 * are dragged over it (a dragenter/dragleave counter keeps child elements
 * from flickering it). Drops while `disabled()` are ignored; a drop of
 * several files hands over the first.
 */
export class FileDrop {
	#depth = 0;
	#over = $state(false);
	#onfile: (f: File) => void;
	#disabled: () => boolean;

	constructor(onfile: (f: File) => void, disabled: () => boolean = () => false) {
		this.#onfile = onfile;
		this.#disabled = disabled;
	}

	get over() {
		return this.#over && !this.#disabled();
	}

	/** Use as `{@attach drop.attach}`. */
	attach = (node: HTMLElement) => {
		const enter = (e: DragEvent) => {
			if (!hasFiles(e)) return;
			e.preventDefault();
			this.#depth++;
			this.#over = true;
		};
		const over = (e: DragEvent) => {
			if (!hasFiles(e)) return;
			e.preventDefault();
			if (e.dataTransfer) e.dataTransfer.dropEffect = this.#disabled() ? 'none' : 'copy';
		};
		const leave = (e: DragEvent) => {
			if (!hasFiles(e)) return;
			this.#depth = Math.max(0, this.#depth - 1);
			if (this.#depth === 0) this.#over = false;
		};
		const drop = (e: DragEvent) => {
			if (!hasFiles(e)) return;
			e.preventDefault();
			this.#depth = 0;
			this.#over = false;
			if (this.#disabled()) return;
			const f = e.dataTransfer?.files[0];
			if (f) this.#onfile(f);
		};
		node.addEventListener('dragenter', enter);
		node.addEventListener('dragover', over);
		node.addEventListener('dragleave', leave);
		node.addEventListener('drop', drop);
		return () => {
			node.removeEventListener('dragenter', enter);
			node.removeEventListener('dragover', over);
			node.removeEventListener('dragleave', leave);
			node.removeEventListener('drop', drop);
		};
	};
}

/** Page-level guard: a file dropped outside a drop target must not navigate away. */
export function guardPageDrops(e: DragEvent) {
	if (!hasFiles(e) || e.defaultPrevented) return;
	e.preventDefault();
	if (e.type === 'dragover' && e.dataTransfer) e.dataTransfer.dropEffect = 'none';
}
