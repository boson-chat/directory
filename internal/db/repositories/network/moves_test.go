package network

import (
	"fmt"
	"slices"
	"testing"
)

func TestPreviousHostnames(t *testing.T) {
	cases := []struct {
		prev     []string
		from, to string
		want     []string
	}{
		{nil, "a", "b", []string{"a"}},
		{[]string{"x", "y"}, "a", "b", []string{"a", "x", "y"}},
		// Moving back to a previous hostname drops it; no duplicates.
		{[]string{"b", "x"}, "a", "b", []string{"a", "x"}},
		{[]string{"a", "x"}, "a", "b", []string{"a", "x"}},
		{[]string{"x"}, "", "b", []string{"x"}},
	}
	for _, c := range cases {
		if got := PreviousHostnames(c.prev, c.from, c.to); !slices.Equal(got, c.want) {
			t.Errorf("PreviousHostnames(%q, %q, %q) = %q, want %q", c.prev, c.from, c.to, got, c.want)
		}
	}
	var long []string
	for i := 0; i < 20; i++ {
		long = append(long, fmt.Sprintf("h%d", i))
	}
	if got := PreviousHostnames(long, "a", "b"); len(got) != MaxPreviousHostnames || got[0] != "a" || got[1] != "h0" {
		t.Fatalf("cap: %q", got)
	}
}
