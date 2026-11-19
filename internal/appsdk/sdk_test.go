package appsdk

import (
	"fmt"
	"strings"
	"testing"
)

func TestCheck(t *testing.T) {
	supported := []string{Version(), fmt.Sprintf("%d.0", Major), fmt.Sprintf("%d.0", MinMajor)}
	for _, version := range supported {
		if err := Check(version); err != nil {
			t.Fatalf("expected %s supported, got %v", version, err)
		}
	}
	cases := map[string]string{
		fmt.Sprintf("%d.9", MinMajor-1):       "no longer supported",
		fmt.Sprintf("%d.0", Major+1):          "newer than",
		fmt.Sprintf("%d.%d", Major, Minor+1):  "newer than",
		fmt.Sprintf("%d.%d", Major, Minor+99): "newer than",
		"1":                                   "MAJOR.MINOR",
		"":                                    "MAJOR.MINOR",
		"a.b":                                 "MAJOR.MINOR",
		"1.x":                                 "MAJOR.MINOR",
		"-1.0":                                "MAJOR.MINOR",
		"1.-1":                                "MAJOR.MINOR",
	}
	for version, want := range cases {
		err := Check(version)
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("expected Check(%q) error containing %q, got %v", version, want, err)
		}
	}
}

func TestVersionAndFiles(t *testing.T) {
	if Version() != fmt.Sprintf("%d.%d", Major, Minor) {
		t.Fatalf("unexpected version %q", Version())
	}
	if MinMajor > Major {
		t.Fatalf("MinMajor %d exceeds Major %d", MinMajor, Major)
	}
	if _, ok := Files()["autable.ts"]; !ok {
		t.Fatalf("expected embedded autable.ts, got %v", Files())
	}
}
