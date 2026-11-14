package integration

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
)

type listResp struct {
	Networks []struct {
		ID        string `json:"id"`
		Hostname  string `json:"hostname"`
		Name      string `json:"name"`
		UserCount *int   `json:"user_count"`
		Status    string `json:"status"`
		Origin    string `json:"origin"`
		Owner     *struct {
			Kind string `json:"kind"`
		} `json:"owner"`
	} `json:"networks"`
	NextCursor    *string `json:"next_cursor"`
	TotalEstimate int64   `json:"total_estimate"`
}

func (e *env) list(query string) listResp {
	e.t.Helper()
	var out listResp
	e.expect(e.do("GET", "/v1/networks?"+query, "", nil), 200).json(e.t, &out)
	return out
}

// seed inserts n verified networks directly, with varied sort keys.
func (e *env) seed(prefix string, n int) {
	e.t.Helper()
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < n; i++ {
		var count any
		if i%4 != 0 {
			count = (i * 7) % 5 // many ties
		}
		var countAt any
		if i%3 == 0 {
			countAt = base.Add(time.Duration(i%5) * time.Hour)
		}
		e.exec(`INSERT INTO networks (id, hostname, port, name, verification_token, status, user_count, user_count_at, created_at, last_verified_at)
			VALUES ($1, $2, 6697, $3, 'tok', 'verified', $4, $5, $6, now())`,
			fmt.Sprintf("net_%s%03d", prefix, i), fmt.Sprintf("irc%d.%s.example", i, prefix),
			fmt.Sprintf("%s %c", prefix, 'A'+rune(i%3)), count, countAt, base.Add(time.Duration(i%6)*time.Minute))
	}
}

func TestPaginationStableUnderInserts(t *testing.T) {
	e := newEnv(t)
	e.seed("orig", 40)
	for _, sort := range []string{"users", "active", "newest", "name"} {
		t.Run(sort, func(t *testing.T) {
			seen := map[string]int{}
			var order []string
			q := "limit=7&sort=" + sort
			pages := 0
			for {
				page := e.list(q)
				for _, n := range page.Networks {
					seen[n.ID]++
					order = append(order, n.ID)
				}
				pages++
				if pages == 2 {
					// Inserts that sort before, between and after the cursor.
					e.seed("ins"+sort, 9)
				}
				if page.NextCursor == nil {
					break
				}
				if pages > 50 {
					t.Fatal("pagination doesn't end")
				}
				q = "limit=7&sort=" + sort + "&cursor=" + url.QueryEscape(*page.NextCursor)
			}
			for i := 0; i < 40; i++ {
				id := fmt.Sprintf("net_orig%03d", i)
				if seen[id] != 1 {
					t.Fatalf("%s seen %d times", id, seen[id])
				}
			}
			for id, c := range seen {
				if c != 1 {
					t.Fatalf("%s seen %d times", id, c)
				}
			}
			// Without concurrent inserts, a full walk equals one big page.
			all := e.list("limit=100&sort=" + sort)
			var want []string
			for _, n := range all.Networks {
				if strings.HasPrefix(n.ID, "net_orig") {
					want = append(want, n.ID)
				}
			}
			var got []string
			for _, id := range order {
				if strings.HasPrefix(id, "net_orig") {
					got = append(got, id)
				}
			}
			if !slices.Equal(got, want) {
				t.Fatalf("paged order differs from single page:\n%v\n%v", got, want)
			}
		})
	}
	// Bad cursors.
	e.expect(e.do("GET", "/v1/networks?cursor=nonsense", "", nil), 400)
	p := e.list("limit=2&sort=name")
	r := e.expect(e.do("GET", "/v1/networks?sort=users&cursor="+url.QueryEscape(*p.NextCursor), "", nil), 400)
	if r.m(t)["error"] != "invalid_cursor" {
		t.Fatalf("got %s", r.Body)
	}
	e.expect(e.do("GET", "/v1/networks?sort=bogus", "", nil), 400)
	e.seed("more", 40)
	if got := e.list("limit=500"); len(got.Networks) != 100 || got.TotalEstimate != 116 || got.NextCursor == nil {
		t.Fatalf("limit clamps to 100: got %d of %d", len(got.Networks), got.TotalEstimate)
	}
}

func TestSearchFacetsAndFilters(t *testing.T) {
	e := newEnv(t)
	k := netkey("key-for-search-tests-0123456789")
	e.registerVerified(k, "irc.libera.chat", map[string]any{"name": "Libera.Chat", "description": "Free and open source communities", "tags": []string{"foss", "chat"}, "languages": []string{"en"}})
	e.registerVerified(k, "irc.oftc.net", map[string]any{"name": "OFTC", "description": "Open and free technology", "tags": []string{"foss"}, "languages": []string{"en", "de"}})
	e.registerVerified(k, "irc.spicy.example", map[string]any{"name": "Spicy", "is_nsfw": true, "tags": []string{"adult"}})
	e.register(k, "irc.pending.example", map[string]any{"name": "Pending libera clone"})

	cases := map[string][]string{
		"q=libera":                         {"irc.libera.chat"},
		"q=libera.chat":                    {"irc.libera.chat"},
		"q=lib":                            {"irc.libera.chat"},
		"q=open":                           {"irc.libera.chat", "irc.oftc.net"},
		"q=open+source":                    {"irc.libera.chat"},
		"q=oftc":                           {"irc.oftc.net"},
		"q=" + url.QueryEscape(`'&|!:*()`): {"irc.libera.chat", "irc.oftc.net"},
		"tag=foss":                         {"irc.libera.chat", "irc.oftc.net"},
		"lang=de":                          {"irc.oftc.net"},
		"q=spicy":                          {},
		"q=spicy&nsfw=true":                {"irc.spicy.example"},
		"source=local&q=oftc":              {"irc.oftc.net"},
		"source=peer:x":                    {},
	}
	for q, want := range cases {
		got := []string{}
		for _, n := range e.list(q + "&sort=name").Networks {
			got = append(got, n.Hostname)
		}
		slices.Sort(got)
		slices.Sort(want)
		if !slices.Equal(got, want) {
			t.Errorf("%s: got %v, want %v", q, got, want)
		}
	}
	var f struct {
		Tags []struct {
			Value string
			Count int
		} `json:"tags"`
		Languages []struct {
			Value string
			Count int
		} `json:"languages"`
	}
	e.expect(e.do("GET", "/v1/facets", "", nil), 200).json(t, &f)
	if len(f.Tags) != 2 || f.Tags[0].Value != "foss" || f.Tags[0].Count != 2 {
		t.Fatalf("tags %+v", f.Tags)
	}
	if len(f.Languages) != 2 || f.Languages[0].Value != "en" || f.Languages[0].Count != 2 {
		t.Fatalf("languages %+v", f.Languages)
	}
	e.expect(e.do("GET", "/v1/facets?nsfw=true", "", nil), 200).json(t, &f)
	if len(f.Tags) != 3 {
		t.Fatalf("nsfw tags %+v", f.Tags)
	}
	// Well-known.
	wk := e.expect(e.do("GET", "/.well-known/boson-directory.json", "", nil), 200).m(t)
	if wk["federation"] != true || wk["api"] != "https://directory.test/v1" || wk["name"] != "test directory" {
		t.Fatalf("well-known %v", wk)
	}
	// CORS * for anonymous reads.
	r := e.do("GET", "/v1/networks", "", nil, "Origin: https://elsewhere.example")
	if r.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("CORS header %q", r.Header.Get("Access-Control-Allow-Origin"))
	}
}

func TestNetworkKeyPath(t *testing.T) {
	e := newEnv(t)
	k := netkey("stack-admin-key-aaaaaaaaaaaaaaaaaaaaaaa")
	other := netkey("stack-admin-key-bbbbbbbbbbbbbbbbbbbbbbb")
	body := map[string]any{"hostname": "IRC.Example.ORG.", "port": 6697, "tls": true, "name": "Example", "website_url": "https://example.org"}
	r := e.expect(e.do("POST", "/v1/networks", k, body), 201)
	l := r.m(t)
	n := l["network"].(map[string]any)
	v := l["verification"].(map[string]any)
	if n["hostname"] != "irc.example.org" || n["status"] != "pending" || n["origin"] != "local" {
		t.Fatalf("network %v", n)
	}
	if v["record_name"] != "_boson.irc.example.org" || !strings.HasPrefix(v["record_value"].(string), "boson-verify=") || v["expires_at"] == nil {
		t.Fatalf("verification %v", v)
	}
	id := n["id"].(string)
	// Idempotent: same key + host → 200, same row.
	r2 := e.expect(e.do("POST", "/v1/networks", k, body), 200).m(t)
	if r2["network"].(map[string]any)["id"] != id {
		t.Fatal("re-register returned a different row")
	}
	// Another key → 409 hostname_taken.
	if e.expect(e.do("POST", "/v1/networks", other, body), 409).m(t)["error"] != "hostname_taken" {
		t.Fatal("want hostname_taken")
	}
	// Lookup: own key sees the pending listing with verification; another key 404; public 404 (pending).
	lk := e.expect(e.do("GET", "/v1/networks/lookup?hostname=irc.example.org", k, nil), 200).m(t)
	if lk["verification"] == nil {
		t.Fatal("owner lookup lacks verification")
	}
	if e.expect(e.do("GET", "/v1/networks/lookup?hostname=irc.example.org", other, nil), 404).m(t)["error"] != "not_found" {
		t.Fatal("want not_found")
	}
	e.expect(e.do("GET", "/v1/networks/lookup?hostname=irc.example.org", "", nil), 404)
	// GET /{id} no longer leaks pending rows.
	e.expect(e.do("GET", "/v1/networks/"+id, "", nil), 404)
	e.expect(e.do("GET", "/v1/networks/"+id, other, nil), 404)
	// Edit: PATCH ignores empty name (admin sends omitempty), hostname is fixed.
	p := e.expect(e.do("PATCH", "/v1/networks/"+id, k, map[string]any{"name": "", "description": "Hello", "tags": []string{"Gaming"}, "hostname": "evil.example"}), 200).m(t)
	pn := p["network"].(map[string]any)
	if pn["name"] != "Example" || pn["description"] != "Hello" || pn["hostname"] != "irc.example.org" || pn["tags"].([]any)[0] != "gaming" {
		t.Fatalf("patched %v", pn)
	}
	e.expect(e.do("PATCH", "/v1/networks/"+id, k, map[string]any{"website_url": "http://insecure"}), 400)
	e.expect(e.do("PATCH", "/v1/networks/"+id, other, map[string]any{"description": "x"}), 404)
	// Verify: fake DNS on 2/3 → 409 with the matrix; strict needs 3/3.
	e.dns.Set("irc.example.org", 2)
	vr := e.expect(e.do("POST", "/v1/networks/"+id+"/verify", k, nil), 409).m(t)
	rep := vr["report"].(map[string]any)
	if vr["success"] != false || rep["success"] != false || len(rep["results"].(map[string]any)) != 3 || vr["network"] == nil || vr["server"] == nil {
		t.Fatalf("verify 409 body %v", vr)
	}
	// Rate limit 1/30s per network.
	r429 := e.expect(e.do("POST", "/v1/networks/"+id+"/verify", k, nil), 429)
	if r429.Header.Get("Retry-After") == "" {
		t.Fatal("no Retry-After")
	}
	e.clock.Advance(31 * time.Second)
	e.dns.Set("irc.example.org", 3)
	ok := e.expect(e.do("POST", "/v1/networks/"+id+"/verify", k, nil), 200).m(t)
	if ok["success"] != true || ok["network"].(map[string]any)["status"] != "verified" {
		t.Fatalf("verify 200 body %v", ok)
	}
	// Now public.
	pub := e.expect(e.do("GET", "/v1/networks/lookup?hostname=irc.example.org", "", nil), 200).m(t)
	if pub["verification"] != nil || pub["network"].(map[string]any)["owner"] != nil {
		t.Fatalf("public lookup leaks owner data: %v", pub)
	}
	// Mine.
	var mine struct{ Listings []map[string]any }
	e.expect(e.do("GET", "/v1/me/networks", k, nil), 200).json(t, &mine)
	if len(mine.Listings) != 1 {
		t.Fatalf("mine %v", mine)
	}
	// Regenerate → pending with a new token.
	rg := e.expect(e.do("POST", "/v1/networks/"+id+"/regenerate-token", k, nil), 200).m(t)
	if rg["network"].(map[string]any)["status"] != "pending" || rg["verification"].(map[string]any)["token"] == v["token"] {
		t.Fatalf("regenerate %v", rg)
	}
	// Delete: wrong key 404, owner 204, then gone.
	e.expect(e.do("DELETE", "/v1/networks/"+id, other, nil), 404)
	e.expect(e.do("DELETE", "/v1/networks/"+id, k, nil), 204)
	e.expect(e.do("GET", "/v1/networks/lookup?hostname=irc.example.org", k, nil), 404)
	// Short keys and unknown schemes are rejected.
	e.expect(e.do("POST", "/v1/networks", "Network short", body), 401)
	e.expect(e.do("POST", "/v1/networks", "Basic abc", body), 401)
	// Invalid hostnames.
	for _, h := range []string{"127.0.0.1", "localhost", "[::1]", "bad_host.example", ""} {
		e.expect(e.do("POST", "/v1/networks", k, map[string]any{"hostname": h, "port": 6697, "name": "x"}), 400)
	}
}

func TestAccountAuthAndOperators(t *testing.T) {
	e := newEnv(t)
	alice, bob := e.token("usr_alice"), e.token("usr_bob")
	op := e.token("usr_op", "directory.manage")
	viewer := e.token("usr_viewer", "users.view")

	e.expect(e.do("POST", "/v1/networks", "", map[string]any{"hostname": "irc.a.example", "port": 6697, "name": "A"}), 401)
	e.expect(e.do("POST", "/v1/networks", "Bearer not-a-jwt", map[string]any{"hostname": "irc.a.example", "port": 6697, "name": "A"}), 401)
	id := e.registerVerified(bearer(alice), "irc.a.example", nil)
	pid, _ := e.register(bearer(alice), "irc.pending-a.example", nil)

	// Bob: sees the verified one publicly, can't edit it (403), can't see the pending one (404).
	e.expect(e.do("GET", "/v1/networks/"+id, bearer(bob), nil), 200)
	if e.expect(e.do("PATCH", "/v1/networks/"+id, bearer(bob), map[string]any{"name": "pwned"}), 403).m(t)["error"] != "not_owner" {
		t.Fatal("want not_owner")
	}
	e.expect(e.do("GET", "/v1/networks/"+pid, bearer(bob), nil), 404)
	// Operator: may edit and see anything.
	e.expect(e.do("PATCH", "/v1/networks/"+id, bearer(op), map[string]any{"name": "Renamed by op"}), 200)
	e.expect(e.do("GET", "/v1/networks/"+pid, bearer(op), nil), 200)
	// owner= needs users.view (or the owner themself).
	e.expect(e.do("GET", "/v1/networks?owner=usr_alice", bearer(bob), nil), 403)
	e.expect(e.do("GET", "/v1/networks?owner=usr_alice", "", nil), 403)
	var lr listResp
	e.expect(e.do("GET", "/v1/networks?owner=usr_alice", bearer(viewer), nil), 200).json(t, &lr)
	if len(lr.Networks) != 2 || lr.Networks[0].Owner == nil {
		t.Fatalf("owner list %+v", lr)
	}
	e.expect(e.do("GET", "/v1/networks?owner=usr_alice", bearer(alice), nil), 200)
	// Internal token (stack admin panel): operator + users.view.
	e.expect(e.do("GET", "/v1/networks?owner=usr_alice", "", nil, "X-Internal-Token: "+internalToken), 200).json(t, &lr)
	if len(lr.Networks) != 2 {
		t.Fatalf("internal owner list %+v", lr)
	}
	e.expect(e.do("GET", "/v1/admin/blocks", "", nil, "X-Internal-Token: wrong"), 401)
	e.expect(e.do("POST", "/v1/admin/networks/"+pid+"/block", "", nil, "X-Internal-Token: "+internalToken, "X-Acting-User: admin1"), 200)
	// Operator endpoints need directory.manage.
	e.expect(e.do("GET", "/v1/admin/networks", bearer(bob), nil), 403)
	e.expect(e.do("GET", "/v1/admin/networks", "", nil), 401)
	e.expect(e.do("GET", "/v1/admin/networks", netkey("stack-admin-key-aaaaaaaaaaaaaaaaaaaaaaa"), nil), 401)
	var all listResp
	e.expect(e.do("GET", "/v1/admin/networks?limit=1", bearer(op), nil), 200).json(t, &all)
	if all.TotalEstimate != 2 || all.NextCursor == nil {
		t.Fatalf("admin list %+v", all)
	}
	e.expect(e.do("GET", "/v1/admin/networks?limit=1&cursor="+url.QueryEscape(*all.NextCursor), bearer(op), nil), 200)
	// Cookie auth: reads work, writes need X-Requested-With.
	cookie := "Cookie: boson_at=" + alice
	e.expect(e.do("GET", "/v1/me/networks", "", nil, cookie), 200)
	e.expect(e.do("PATCH", "/v1/networks/"+pid, "", map[string]any{"description": "x"}, cookie), 403)
	// Audit has the actions with actors.
	var au struct {
		Entries []struct {
			Action    string `json:"action"`
			ActorKind string `json:"actor_kind"`
			ActorID   string `json:"actor_id"`
		} `json:"entries"`
	}
	e.expect(e.do("GET", "/v1/admin/audit?limit=100", bearer(op), nil), 200).json(t, &au)
	var actions []string
	for _, a := range au.Entries {
		actions = append(actions, a.Action+"/"+a.ActorKind+"/"+a.ActorID)
	}
	for _, want := range []string{"network.create/account/usr_alice", "network.update/operator/usr_op", "network.block/operator/internal:admin1"} {
		if !slices.Contains(actions, want) {
			t.Errorf("audit lacks %s: %v", want, actions)
		}
	}
	// Internal token can't own listings.
	e.expect(e.do("POST", "/v1/networks", "", map[string]any{"hostname": "irc.i.example", "port": 6697, "name": "I"}, "X-Internal-Token: "+internalToken), 403)
}

func TestBlocks(t *testing.T) {
	e := newEnv(t)
	k := netkey("stack-admin-key-ccccccccccccccccccccccc")
	op := bearer(e.token("usr_op", "directory.manage"))
	id := e.registerVerified(k, "irc.bad.example", nil)
	sub := e.registerVerified(k, "chat.irc.bad.example", nil)
	keep := e.registerVerified(k, "irc.good.example", nil)

	e.expect(e.do("POST", "/v1/admin/blocks", op, map[string]any{"pattern": "not a host"}), 400)
	r := e.expect(e.do("POST", "/v1/admin/blocks", op, map[string]any{"pattern": "*.bad.example", "reason": "spam"}), 201).m(t)
	if len(r["blocked_network_ids"].([]any)) != 2 {
		t.Fatalf("blocked %v", r)
	}
	e.expect(e.do("POST", "/v1/admin/blocks", op, map[string]any{"pattern": "*.BAD.example"}), 409)
	if e.status(id) != "blocked" || e.status(sub) != "blocked" || e.status(keep) != "verified" {
		t.Fatal("block didn't apply")
	}
	if len(e.list("q=bad").Networks) != 0 {
		t.Fatal("blocked listing still public")
	}
	if e.expect(e.do("POST", "/v1/networks", k, map[string]any{"hostname": "new.bad.example", "port": 6697, "name": "x"}), 403).m(t)["error"] != "blocked" {
		t.Fatal("want blocked")
	}
	e.expect(e.do("POST", "/v1/networks/"+id+"/verify", k, nil), 403)
	// Unblocking one listing under a pattern is refused.
	e.expect(e.do("POST", "/v1/admin/networks/"+id+"/unblock", op, nil), 409)
	bid := r["block"].(map[string]any)["id"].(string)
	e.expect(e.do("DELETE", "/v1/admin/blocks/"+bid, op, nil), 204)
	if e.status(id) != "lapsed" {
		t.Fatalf("released listing is %s, want lapsed (re-check due)", e.status(id))
	}
	e.runWorker("verify")
	if e.status(id) != "verified" {
		t.Fatalf("after re-check %s", e.status(id))
	}
}

func TestImages(t *testing.T) {
	e := newEnv(t)
	k := netkey("stack-admin-key-ddddddddddddddddddddddd")
	id, _ := e.register(k, "irc.img.example", nil)
	e.expect(e.do("PUT", "/v1/networks/"+id+"/icon", k, []byte("not an image")), 400)
	r := e.expect(e.do("PUT", "/v1/networks/"+id+"/icon", k, pngBytes(t, 40, 20)), 200).m(t)
	icon, _ := r["network"].(map[string]any)["icon_url"].(string)
	if !strings.HasPrefix(icon, "https://media.test/network-icons/"+id+"-") {
		t.Fatalf("icon_url %q", icon)
	}
	if len(e.media.Objects) != 1 {
		t.Fatalf("objects %d", len(e.media.Objects))
	}
	e.expect(e.do("PUT", "/v1/networks/"+id+"/banner", k, pngBytes(t, 300, 100)), 200)
	e.expect(e.do("PUT", "/v1/networks/"+id+"/icon", netkey("someone-else-key-0000000000000"), pngBytes(t, 4, 4)), 404)
	d := e.expect(e.do("DELETE", "/v1/networks/"+id+"/icon", k, nil), 200).m(t)
	if _, has := d["network"].(map[string]any)["icon_url"]; has {
		t.Fatal("icon not cleared")
	}
	if len(e.media.Objects) != 1 {
		t.Fatalf("objects after delete %d", len(e.media.Objects))
	}
	e.expect(e.do("DELETE", "/v1/networks/"+id, k, nil), 204)
	if len(e.media.Objects) != 0 {
		t.Fatalf("objects after network delete %d", len(e.media.Objects))
	}
}
