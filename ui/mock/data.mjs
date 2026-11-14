// Seed data for the mock directory: deterministic, so screenshots are stable.

const NOW = Date.parse('2026-11-14T12:00:00Z');
const hours = (h) => new Date(NOW - h * 3600_000).toISOString();

const palette = ['#d97706', '#10b981', '#3b82f6', '#a855f7', '#ef4444', '#14b8a6', '#eab308', '#ec4899'];

export function iconSvg(i, letter) {
	const c = palette[i % palette.length];
	return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 64"><rect width="64" height="64" fill="${c}"/><text x="32" y="42" font-family="monospace" font-size="30" font-weight="700" text-anchor="middle" fill="#070709">${letter}</text></svg>`;
}

export function bannerSvg(i) {
	const a = palette[i % palette.length];
	const b = palette[(i + 3) % palette.length];
	let lines = '';
	for (let k = 0; k < 14; k++) {
		lines += `<path d="M${-40 + k * 30} 200 L${80 + k * 30} 0" stroke="${b}" stroke-opacity="0.25" stroke-width="2"/>`;
	}
	return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 600 200" preserveAspectRatio="xMidYMid slice"><defs><linearGradient id="g" x1="0" x2="1"><stop offset="0" stop-color="${a}" stop-opacity="0.55"/><stop offset="1" stop-color="${b}" stop-opacity="0.2"/></linearGradient></defs><rect width="600" height="200" fill="#141418"/><rect width="600" height="200" fill="url(#g)"/>${lines}</svg>`;
}

const names = [
	[
		'Libera Lounge',
		'libera-lounge.example',
		['community', 'tech', 'open-source'],
		['en'],
		'A friendly general-purpose network for open-source people and the curious.'
	],
	[
		'Rust Belt',
		'irc.rustbelt.example',
		['programming', 'rust'],
		['en'],
		'Systems programmers, borrow-checker therapy and weekly code reviews.'
	],
	[
		'Café Crème',
		'irc.cafecreme.example',
		['chat', 'social'],
		['fr'],
		'Discussions en français, du matin au soir.'
	],
	[
		'Nordlys',
		'nordlys.example',
		['gaming', 'chat'],
		['no', 'sv', 'da'],
		'Scandinavian gamers and night owls.'
	],
	[
		'Pixel Forge',
		'irc.pixelforge.example',
		['art', 'gamedev'],
		['en'],
		'Pixel art critiques, jams and tool talk.'
	],
	[
		'Retro Terminal',
		'retro.example',
		['retro', 'computing'],
		['en', 'de'],
		'BBS nostalgia, vintage hardware and ANSI art.'
	],
	[
		'Hackerspace Berlin',
		'irc.hsb.example',
		['hackerspace', 'tech'],
		['de', 'en'],
		'Der IRC unseres Hackerspaces. Gäste willkommen.'
	],
	[
		'Kanji Corner',
		'irc.kanji.example',
		['language-learning'],
		['ja', 'en'],
		'Learn Japanese together. Daily vocabulary drills.'
	],
	['Synthwave', 'synthwave.example', ['music'], ['en'], 'Neon, arpeggios and modular rigs.'],
	[
		'Linux Help Desk',
		'irc.lhd.example',
		['support', 'linux'],
		['en'],
		'Ask anything about Linux. Be patient, paste your logs.'
	],
	['Mate Amargo', 'irc.mate.example', ['chat', 'social'], ['es'], 'La red de charla rioplatense.'],
	[
		'Orbit',
		'orbit.example',
		['space', 'science'],
		['en'],
		'Launch threads, amateur astronomy and orbital mechanics.'
	],
	[
		'Bookworms',
		'irc.bookworms.example',
		['books', 'social'],
		['en'],
		'A slow-paced reading club with monthly picks.'
	],
	[
		'Cozy Cats',
		'cozycats.example',
		['pets', 'social'],
		['en', 'pt'],
		'Photos of cats. That is the whole network.'
	],
	['Sakura Net', 'irc.sakura.example', ['anime', 'chat'], ['ja'], 'アニメと雑談。'],
	[
		'Packet Radio',
		'irc.packet.example',
		['ham-radio', 'tech'],
		['en'],
		'Amateur radio operators and APRS tinkerers.'
	],
	['Velo', 'velo.example', ['cycling', 'sports'], ['it', 'en'], 'Ciclismo, gare e uscite di gruppo.'],
	[
		'Darkroom',
		'irc.darkroom.example',
		['photography', 'art'],
		['en'],
		'Film photography, developing and scanning tips.'
	],
	[
		'After Hours',
		'afterhours.example',
		['adult', 'social'],
		['en'],
		'Adults only. Read the rules channel first.'
	],
	[
		'Polyglot',
		'polyglot.example',
		['language-learning', 'social'],
		['en', 'es', 'fr', 'de'],
		'Practise any language with native speakers.'
	],
	[
		'Homelab',
		'irc.homelab.example',
		['selfhosting', 'tech'],
		['en'],
		'Racks, NAS builds and too many Raspberry Pis.'
	],
	[
		'Chess Club',
		'chess.example',
		['games', 'chess'],
		['en', 'ru'],
		'Puzzles, openings and weekly blitz arenas.'
	],
	['Wiener Melange', 'irc.melange.example', ['chat'], ['de'], 'Plaudern aus Wien und überall.'],
	[
		'Open Data',
		'opendata.example',
		['science', 'open-source'],
		['en'],
		'Civic tech, public datasets and visualisation.'
	],
	['Tea House', 'teahouse.example', ['social'], ['zh', 'en'], '茶馆：慢慢聊。'],
	['Seoul Station', 'irc.seoul.example', ['chat', 'music'], ['ko'], '케이팝과 수다.'],
	['Midnight Lab', 'midnight.example', ['adult', 'art'], ['en'], 'Adult art and writing. 18+.'],
	['Garden Path', 'gardenpath.example', ['gardening'], ['en'], 'Allotments, seeds and composting debates.'],
	[
		'Kernel Panic',
		'irc.kpanic.example',
		['programming', 'linux'],
		['en'],
		'Kernel hackers and driver writers.'
	],
	['Brasil Livre', 'irc.brlivre.example', ['open-source', 'chat'], ['pt'], 'Software livre e bate-papo.']
];

export const peers = [
	{
		id: 'peer_main',
		url: 'https://directory.boson.chat',
		name: 'boson directory',
		enabled: true,
		builtin: true,
		cursor: 18342,
		last_sync_at: hours(0.1),
		last_error: null,
		listing_count: 0,
		created_at: hours(2000)
	},
	{
		id: 'peer_fr',
		url: 'https://annuaire.irc-fr.example',
		name: 'Annuaire IRC-FR',
		enabled: true,
		builtin: false,
		cursor: 311,
		last_sync_at: hours(0.2),
		last_error: null,
		listing_count: 0,
		created_at: hours(900)
	},
	{
		id: 'peer_old',
		url: 'https://dir.oldnet.example',
		name: 'oldnet',
		enabled: false,
		builtin: false,
		cursor: 12,
		last_sync_at: hours(240),
		last_error: 'GET /v1/feed: dial tcp: lookup dir.oldnet.example: no such host',
		listing_count: 0,
		created_at: hours(1500)
	}
];

export function seedNetworks() {
	const out = [];
	let n = 0;
	// 30 named + 42 generated so the list pages a few times.
	for (let round = 0; round < 3; round++) {
		for (const [name, host, tags, languages, description] of names) {
			if (round === 2 && n % 3) {
				n++;
				continue;
			}
			const i = n++;
			const suffix = round === 0 ? '' : ` ${['', 'II', 'III'][round]}`;
			const hostname = round === 0 ? host : host.replace(/^(irc\.)?/, `$1r${round}-`);
			const origin = i % 5 === 3 ? 'peer:peer_main' : i % 7 === 4 ? 'peer:peer_fr' : 'local';
			const health = i % 9 === 5 ? 'down' : i % 13 === 7 ? 'unknown' : 'up';
			out.push({
				id: `net_${String(i).padStart(4, '0')}`,
				hostname,
				port: i % 4 === 0 ? 6667 : 6697,
				tls: i % 4 !== 0,
				name: name + suffix,
				description,
				tags,
				languages,
				is_nsfw: tags.includes('adult'),
				website_url: i % 3 === 0 ? `https://${hostname.replace(/^irc\./, '')}` : null,
				auth_url: null,
				icon_url: i % 6 === 2 ? null : `/media/icon-${i}.svg`,
				banner_url: i % 4 === 1 ? null : `/media/banner-${i}.svg`,
				status: 'verified',
				origin,
				origin_name: origin === 'local' ? null : peers.find((p) => 'peer:' + p.id === origin)?.name,
				health_status: health,
				health_checked_at: hours(0.2),
				user_count: health === 'up' ? Math.round(4000 / (i + 1.3) + (i % 7) * 11) : null,
				user_count_at: hours(0.2),
				last_verified_at: hours(5 + (i % 20)),
				created_at: hours(100 + i * 37),
				updated_at: hours(3 + i),
				owner_auth_user_id: 'usr_someone',
				_letter: name[0]
			});
		}
	}
	return out;
}

/** The demo account's own listings, one in each state. */
export function demoOwned() {
	const base = {
		port: 6697,
		tls: true,
		languages: ['en'],
		is_nsfw: false,
		website_url: null,
		auth_url: null,
		icon_url: null,
		banner_url: null,
		origin: 'local',
		health_status: 'unknown',
		user_count: null,
		owner_auth_user_id: 'usr_demo'
	};
	return [
		{
			...base,
			id: 'net_demo_verified',
			hostname: 'irc.thelounge.example',
			name: 'The Lounge',
			description: 'Our small community network. Friendly, moderated, ad-free.',
			tags: ['community', 'chat'],
			status: 'verified',
			health_status: 'up',
			user_count: 142,
			icon_url: '/media/icon-3.svg',
			banner_url: '/media/banner-3.svg',
			last_verified_at: hours(6),
			last_checked_at: hours(6),
			verification_token: null,
			token_issued_at: hours(400),
			created_at: hours(400),
			updated_at: hours(6)
		},
		{
			...base,
			id: 'net_demo_pending',
			hostname: 'irc.newplace.example',
			name: 'New Place',
			description: 'Just set up, waiting on DNS.',
			tags: ['tech'],
			status: 'pending',
			verification_token: 'k7Qm2xVb9pT4wZr8LcN3',
			token_issued_at: hours(5),
			last_checked_at: hours(1),
			created_at: hours(5),
			updated_at: hours(5)
		},
		{
			...base,
			id: 'net_demo_expired',
			hostname: 'irc.expired.example',
			name: 'Forgotten Network',
			description: '',
			tags: [],
			status: 'pending',
			verification_token: 'Zp0oLdQ8sRkW1yUeHf6M',
			token_issued_at: hours(100),
			last_checked_at: null,
			created_at: hours(100),
			updated_at: hours(100)
		},
		{
			...base,
			id: 'net_demo_lapsed',
			hostname: 'irc.wandering.example',
			name: 'Wandering Star',
			description: 'Moved DNS providers and lost the record.',
			tags: ['social'],
			status: 'lapsed',
			verification_token: 'Ab3dEf7gHi9jKl2mNo4p',
			token_issued_at: hours(2),
			last_verified_at: hours(24 * 16),
			last_checked_at: hours(20),
			created_at: hours(3000),
			updated_at: hours(20)
		}
	];
}

export const blocks = [
	{
		id: 'blk_1',
		pattern: '*.spam-farm.example',
		reason: 'Bulk-registered spam networks',
		created_by: 'operator',
		created_at: hours(300)
	},
	{
		id: 'blk_2',
		pattern: 'irc.scam.example',
		reason: 'Phishing links in the MOTD',
		created_by: 'operator',
		created_at: hours(50)
	}
];

export const audit = [
	['peer.sync', 'system', null, { added: 4, updated: 12, removed: 1 }],
	['network.verify', 'system', 'irc.thelounge.example', { success: true }],
	['block.add', 'operator', 'irc.scam.example', { reason: 'Phishing links in the MOTD' }],
	['network.create', 'demo', 'irc.newplace.example', null],
	['network.verify', 'system', 'irc.wandering.example', { success: false, lapsed: true }],
	['peer.disable', 'operator', null, { peer: 'oldnet' }],
	['network.delete', 'demo', 'irc.oldthing.example', null],
	['peer.add', 'operator', null, { url: 'https://annuaire.irc-fr.example' }],
	['network.block', 'operator', 'irc.bad.example', { reason: 'Impersonates another network' }]
].map(([action, actor, hostname, meta], i) => ({
	id: `aud_${i}`,
	at: hours(i * 7 + 0.5),
	action,
	actor: {
		kind: actor === 'system' ? 'system' : 'account',
		id: actor === 'system' ? '' : 'usr_' + actor,
		name: actor
	},
	target: hostname ? { hostname } : {},
	meta
}));
