/** The claims this UI reads from a boson access token. The directory verifies the signature. */
export interface Claims {
	sub: string;
	name?: string;
	perms?: string[];
	exp?: number;
}

/** Decodes a JWT's payload without verifying it (display and UI gating only). */
export function decodeClaims(token: string): Claims | null {
	const part = token.split('.')[1];
	if (!part) return null;
	try {
		const b64 = part.replace(/-/g, '+').replace(/_/g, '/');
		const pad = b64 + '='.repeat((4 - (b64.length % 4)) % 4);
		const bin = atob(pad);
		const bytes = Uint8Array.from(bin, (c) => c.charCodeAt(0));
		const c = JSON.parse(new TextDecoder().decode(bytes));
		return c && typeof c.sub === 'string' ? (c as Claims) : null;
	} catch {
		return null;
	}
}
