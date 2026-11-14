package health

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"
)

func fakeServer(t *testing.T, lines ...string) net.Conn {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		server, err := ln.Accept()
		if err != nil {
			return
		}
		defer server.Close()
		r := bufio.NewReader(server)
		_, _ = r.ReadString('\n') // NICK
		_, _ = r.ReadString('\n') // USER
		for _, l := range lines {
			if _, err := server.Write([]byte(l + "\r\n")); err != nil {
				return
			}
		}
		for {
			if _, err := r.ReadString('\n'); err != nil {
				return
			}
		}
	}()
	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	_ = client.SetDeadline(time.Now().Add(5 * time.Second))
	return client
}

func TestReadLusers(t *testing.T) {
	cases := []struct {
		name  string
		lines []string
		want  int
	}{
		{"266 params", []string{":srv 001 n :Welcome", ":srv 251 n :There are 3 users and 9 invisible on 1 servers", ":srv 266 n 12 30 :Current global users 12, max 30"}, 12},
		{"266 trailing only", []string{":srv 266 n :Current global users: 77  Max: 100"}, 77},
		{"251 fallback", []string{"PING :x", ":srv 251 n :There are 5 users and 7 invisible on 2 servers", ":srv 376 n :End of MOTD"}, 12},
	}
	for _, c := range cases {
		got, any := readLusers(fakeServer(t, c.lines...))
		if !any || got == nil || *got != c.want {
			t.Errorf("%s: %v %v, want %d", c.name, got, any, c.want)
		}
	}
	got, any := readLusers(fakeServer(t, ":srv NOTICE * :hi", "ERROR :Closing link"))
	if !any || got != nil {
		t.Errorf("no counts: %v %v", got, any)
	}
}

func TestParse(t *testing.T) {
	cmd, params, trailing := parse("@time=x :srv.example 266 nick 1 2 :Current global users")
	if cmd != "266" || strings.Join(params, ",") != "nick,1,2" || trailing != "Current global users" {
		t.Fatalf("%q %q %q", cmd, params, trailing)
	}
}
