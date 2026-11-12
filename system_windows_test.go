package main

import "testing"

func TestWindowCornersFollowWindowsBuild(t *testing.T) {
	for _, test := range []struct {
		major, build uint32
		rounded      bool
	}{{10, 19045, false}, {10, 21999, false}, {10, 22000, true}, {10, 22631, true}, {6, 7601, false}} {
		if roundedWindowForVersion(test.major, test.build) != test.rounded {
			t.Fatalf("unexpected frame for Windows %d build %d", test.major, test.build)
		}
	}
}
