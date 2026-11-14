import { ApiError } from './api';
import type { FormErrors } from './validate';

/** Puts a create/edit failure next to the field it's about, when it is about one. */
export function fieldErrors(e: unknown): FormErrors | null {
	if (!(e instanceof ApiError)) return null;
	switch (e.code) {
		case 'hostname_taken':
			return { hostname: e.message || 'That hostname is already listed in this directory.' };
		case 'invalid_hostname':
		case 'hostname_is_ip':
		case 'blocked':
			return { hostname: e.message };
		case 'invalid_port':
			return { port: e.message };
		case 'invalid_name':
			return { name: e.message };
		case 'invalid_website_url':
			return { website_url: e.message };
		default:
			return null;
	}
}
