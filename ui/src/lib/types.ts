import type { BosonTheme } from './theme';

// Shapes of the directory API (/v1), as agreed in the directory plan.
// Fields marked "assumed" are this UI's reading of the contract where the
// plan leaves the exact name open; the typed client is the one place to
// change if the service spells them differently.

export type NetworkStatus = 'pending' | 'verified' | 'lapsed' | 'blocked';
export type HealthStatus = 'up' | 'down' | 'unknown';
export type Sort = 'users' | 'active' | 'newest' | 'name';

/** A listing as the public API returns it. */
export interface Network {
	id: string;
	hostname: string;
	port: number;
	tls: boolean;
	name: string;
	description?: string | null;
	tags: string[];
	languages: string[];
	is_nsfw: boolean;
	website_url?: string | null;
	auth_url?: string | null;
	icon_url?: string | null;
	banner_url?: string | null;
	/** The network's boson-theme/1 branding (absolute image URLs); null when it has none. */
	theme?: BosonTheme | null;
	/** The network's fixed channels (names as written); [] when none. Older directories omit it. */
	channels?: string[];
	/** The channels new users are joined to ("Joins #lobby"); [] when none. */
	autojoin_channels?: string[];
	status: NetworkStatus;
	/** Same as `status`, for clients written against /servers. */
	verification_status?: NetworkStatus;
	/** `local` or `peer:<id>`. */
	origin: string;
	/** The peer's name (or URL) for imported rows; null for local ones. */
	origin_name?: string | null;
	health_status: HealthStatus;
	health_checked_at?: string | null;
	user_count?: number | null;
	user_count_at?: string | null;
	last_verified_at?: string | null;
	last_checked_at?: string | null;
	created_at: string;
	updated_at: string;
	registered_at?: string;
	/** Only for whoever may manage the listing (and in owner/operator lists). */
	owner?: { kind: 'account' | 'network_key' | 'none'; auth_user_id?: string };
	verification_token?: string | null;
	token_issued_at?: string | null;
	/** issued + 72 h. */
	token_expires_at?: string | null;
}

/** Kept as an alias: the owner fields are optional on Network. */
export type OwnedNetwork = Network;

/** The TXT record a listing's managers publish. */
export interface Verification {
	record_name: string;
	record_value: string;
	token: string;
	issued_at: string;
	/** Pending local listings only: verify before this. */
	expires_at?: string | null;
}

/** GET /v1/networks/{id} and every owner write: the network plus, for its managers, the record. */
export interface Listing {
	network: Network;
	verification: Verification | null;
}

export interface NetworkPage {
	networks: Network[];
	next_cursor: string | null;
	total_estimate: number;
}

export interface FacetCount {
	value: string;
	count: number;
}

/** `local` plus each peer; `id` works as `?source=`. */
export interface SourceFacet {
	id: string;
	name: string;
	url?: string;
	count: number;
}

export interface Facets {
	tags: FacetCount[];
	languages: FacetCount[];
	sources?: SourceFacet[];
}

export interface WellKnown {
	name: string;
	api: string;
	version: string;
	operator?: string;
	contact?: string;
	/** The boson auth service people sign in with. */
	auth?: string;
	federation: boolean;
}

export type Outcome = 'match' | 'missing_record' | 'timeout' | 'error';

export interface ResolverResult {
	outcome: Outcome;
	detail?: string;
	records?: string[];
}

export interface VerifyReport {
	success: boolean;
	results: Record<string, ResolverResult>;
}

/** POST /v1/networks/{id}/verify: 200 on success, 409 with the same body when partial. */
export interface VerifyResponse {
	network: Network;
	/** Same as `network`, for /servers clients. */
	server?: Network;
	verification: Verification | null;
	report: VerifyReport;
	success: boolean;
}

export interface NetworkInput {
	hostname: string;
	port: number;
	tls: boolean;
	name: string;
	description: string;
	tags: string[];
	languages: string[];
	is_nsfw: boolean;
	website_url: string;
	channels: string[];
	autojoin_channels: string[];
}

export type NetworkPatch = Partial<Omit<NetworkInput, 'hostname' | 'port' | 'tls'>>;

// ---- operator API (/v1/admin, needs directory.manage) ----

export interface Peer {
	id: string;
	url: string;
	name: string;
	api?: string;
	enabled: boolean;
	/** The main directory every install peers with by default. */
	builtin: boolean;
	/** Enabled and actually synced (not this directory itself, not switched off by env). */
	active?: boolean;
	cursor?: number | null;
	last_sync_at?: string | null;
	last_full_sync_at?: string | null;
	last_error?: string | null;
	listing_count: number;
	created_at?: string;
	updated_at?: string;
}

export interface SyncResult {
	[key: string]: unknown;
}

/** GET /v1/admin/settings: runtime settings (env values are defaults). */
export interface DirectorySettings {
	settings: {
		name: string;
		operator: string;
		contact: string;
		include_main: boolean;
		main_peer_url: string;
		sync_every: string;
	};
	overridden: string[];
	/** This is the main directory itself: include_main doesn't apply. */
	is_main: boolean;
	main_active: boolean;
	main_peer: Peer | null;
}

export interface PeerCheck {
	url: string;
	well_known: WellKnown;
}

export interface Block {
	id: string;
	/** A hostname, or `*.example.org` (the domain and every subdomain). */
	pattern: string;
	reason: string;
	created_by?: string | null;
	created_at: string;
}

export interface AdminNetworkPage {
	networks: Network[];
	next_cursor: string | null;
	total_estimate?: number;
}

export interface AuditEntry {
	id: string;
	at: string;
	actor?: { kind: string; id?: string; name?: string } | null;
	action: string;
	target?: { network_id?: string; hostname?: string } | null;
	meta?: Record<string, unknown> | null;
}

export interface AuditPage {
	entries: AuditEntry[];
	next_cursor: string | null;
}

// ---- auth service ----

export interface AuthUser {
	id: string;
	username: string;
	display_name?: string | null;
	email?: string | null;
}

export interface TokenResponse {
	token_type: string;
	access_token: string;
	expires_in: number;
	refresh_token: string;
	refresh_expires_in: number;
	session_id: string;
	user: AuthUser;
}

export interface SsoProvider {
	slug: string;
	kind: string;
	display_name: string;
	start_url: string;
}

export interface AuthConfig {
	registration: string;
	sso_providers: SsoProvider[];
}

/** What the UI needs from /.well-known/boson-directory.json. */
export interface UiConfig {
	auth_url: string;
	name: string;
}
