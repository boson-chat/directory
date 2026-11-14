export interface Toast {
	id: number;
	text: string;
	tone: 'ok' | 'bad';
}

class Toasts {
	list = $state<Toast[]>([]);
	#next = 1;

	push(text: string, tone: Toast['tone'] = 'ok') {
		const id = this.#next++;
		this.list.push({ id, text, tone });
		setTimeout(() => this.dismiss(id), tone === 'bad' ? 8000 : 4000);
	}

	dismiss(id: number) {
		this.list = this.list.filter((t) => t.id !== id);
	}
}

export const toasts = new Toasts();

/** Message for a failed call, in the panel's voice. */
export function errorText(e: unknown): string {
	if (e && typeof e === 'object' && 'message' in e && typeof e.message === 'string' && e.message)
		return e.message;
	return 'Something went wrong. Try again.';
}
