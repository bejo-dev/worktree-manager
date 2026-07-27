package main

import "testing"

func TestRequireForce(t *testing.T) {
	if err := requireForce([]string{"--force"}); err != nil {
		t.Fatalf("requireForce(--force): %v", err)
	}
	for _, args := range [][]string{nil, {}, {"--force", "extra"}, {"--yes"}} {
		if err := requireForce(args); err == nil {
			t.Fatalf("requireForce(%q) succeeded", args)
		}
	}
}
