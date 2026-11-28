package launcher

import (
	"slices"
	"testing"
)

func TestJVMProfileSelectsOneCompatibleCollector(t *testing.T) {
	for _, test := range []struct {
		name, profile string
		java          int
		custom        []string
		expected      []string
	}{
		{"automatic", "", 21, nil, []string{"-XX:+UseParallelGC", "-Dtest=1"}},
		{"G1GC", "g1gc", 8, nil, []string{"-Dtest=1", "-XX:+UseG1GC"}},
		{"ZGC", "zgc", 21, nil, []string{"-Dtest=1", "-XX:+UseZGC"}},
		{"legacy fallback", "zgc", 8, nil, []string{"-Dtest=1", "-XX:+UseG1GC"}},
		{"custom JVM argument", "zgc", 21, []string{"-XX:+UseParallelGC"}, []string{"-XX:+UseParallelGC", "-Dtest=1"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := applyJVMProfile([]string{"-XX:+UseParallelGC", "-Dtest=1"}, LaunchOptions{JVMProfile: test.profile, JVMArgs: test.custom}, test.java)
			if !slices.Equal(args, test.expected) {
				t.Fatalf("got %v, want %v", args, test.expected)
			}
		})
	}
}
