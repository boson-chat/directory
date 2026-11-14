const rtf = new Intl.RelativeTimeFormat('en', { numeric: 'auto' });
const dtf = new Intl.DateTimeFormat('en', { dateStyle: 'medium', timeStyle: 'short' });
const df = new Intl.DateTimeFormat('en', { dateStyle: 'medium' });

const units: [Intl.RelativeTimeFormatUnit, number][] = [
	['year', 31536000],
	['month', 2592000],
	['week', 604800],
	['day', 86400],
	['hour', 3600],
	['minute', 60],
	['second', 1]
];

/** "3 hours ago", "in 2 days". */
export function relative(iso: string | null | undefined, now = Date.now()): string {
	if (!iso) return '';
	const diff = (new Date(iso).getTime() - now) / 1000;
	if (Math.abs(diff) < 45) return 'just now';
	for (const [unit, secs] of units) {
		if (Math.abs(diff) >= secs || unit === 'second') return rtf.format(Math.round(diff / secs), unit);
	}
	return '';
}

export function datetime(iso: string | null | undefined): string {
	return iso ? dtf.format(new Date(iso)) : '';
}

export function date(iso: string | null | undefined): string {
	return iso ? df.format(new Date(iso)) : '';
}

const nf = new Intl.NumberFormat('en', { maximumFractionDigits: 1 });
export function num(v: number | null | undefined): string {
	if (v === null || v === undefined) return '—';
	if (Math.abs(v) >= 10000)
		return new Intl.NumberFormat('en', { notation: 'compact', maximumFractionDigits: 1 }).format(v);
	return nf.format(v);
}

/** A short browser + OS from a user agent, for session lists. */
export function browser(ua: string): string {
	if (!ua) return 'Unknown client';
	const b = /Edg\//.test(ua)
		? 'Edge'
		: /Firefox\//.test(ua)
			? 'Firefox'
			: /Chrome\//.test(ua)
				? 'Chrome'
				: /Safari\//.test(ua)
					? 'Safari'
					: /curl\//.test(ua)
						? 'curl'
						: /Go-http-client/.test(ua)
							? 'Go client'
							: ua.split(/[\s/]/)[0];
	const os = /Windows/.test(ua)
		? 'Windows'
		: /Mac OS X|Macintosh/.test(ua)
			? 'macOS'
			: /Android/.test(ua)
				? 'Android'
				: /iPhone|iPad/.test(ua)
					? 'iOS'
					: /Linux/.test(ua)
						? 'Linux'
						: '';
	return os ? `${b} on ${os}` : b;
}

/** Human names for audit events. */
const eventNames: Record<string, string> = {
	register: 'Registered',
	email_verified: 'Verified email',
	login: 'Signed in',
	login_failed: 'Failed sign-in',
	logout: 'Signed out',
	refresh_token_reuse: 'Refresh token reused (session revoked)',
	password_reset_requested: 'Requested password reset',
	password_reset: 'Reset password',
	password_changed: 'Changed password',
	session_revoked: 'Revoked a session',
	internal_verify_password: 'IRC password check',
	admin_user_created: 'Created a user',
	admin_user_disabled: 'Disabled a user',
	admin_user_enabled: 'Enabled a user',
	admin_role_changed: 'Changed admin status',
	admin_session_revoked: 'Revoked a user session',
	admin_invite_created: 'Created an invite',
	admin_invite_revoked: 'Revoked an invite',
	settings_updated: 'Changed network settings',
	sso_provider_created: 'Added a sign-in provider',
	sso_provider_updated: 'Changed a sign-in provider',
	sso_provider_deleted: 'Removed a sign-in provider',
	admin_password_reset_sent: 'Sent a password reset',
	admin_verification_sent: 'Resent a verification email',
	sso_identity_linked: 'Linked an SSO sign-in',
	sso_identity_unlinked: 'Unlinked an SSO sign-in',
	sso_login_failed: 'Failed SSO sign-in',
	signing_key_created: 'Created a signing key',
	role_created: 'Created a role',
	role_updated: 'Changed a role',
	role_deleted: 'Deleted a role',
	user_roles_changed: 'Changed someone’s roles'
};
export function eventName(e: string): string {
	return eventNames[e] ?? e.replaceAll('_', ' ');
}

/** Tone of an audit event for its marker. */
export function eventTone(e: string): 'bad' | 'admin' | 'plain' {
	if (e === 'login_failed' || e === 'refresh_token_reuse' || e === 'sso_login_failed') return 'bad';
	if (
		e.startsWith('admin_') ||
		e.startsWith('sso_provider_') ||
		e.startsWith('role_') ||
		e === 'user_roles_changed' ||
		e === 'settings_updated' ||
		e === 'signing_key_created'
	)
		return 'admin';
	return 'plain';
}

/** 1.4 GiB, 312 MiB. */
export function bytes(n: number | null | undefined): string {
	if (n === null || n === undefined) return '—';
	const u = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
	let i = 0;
	let v = n;
	while (v >= 1024 && i < u.length - 1) {
		v /= 1024;
		i++;
	}
	return `${v >= 100 || i === 0 ? Math.round(v) : v.toFixed(1)} ${u[i]}`;
}

/** Elapsed time since iso, compact: 3d 4h, 2h 5m, 40s. */
export function since(iso: string | null | undefined, now = Date.now()): string {
	if (!iso) return '';
	let s = Math.max(0, Math.floor((now - new Date(iso).getTime()) / 1000));
	const d = Math.floor(s / 86400);
	s -= d * 86400;
	const h = Math.floor(s / 3600);
	s -= h * 3600;
	const m = Math.floor(s / 60);
	s -= m * 60;
	if (d) return h ? `${d}d ${h}h` : `${d}d`;
	if (h) return m ? `${h}h ${m}m` : `${h}h`;
	if (m) return `${m}m`;
	return `${s}s`;
}
