package selfupdate

import "testing"

func TestVersionOrdering(t *testing.T) {
	t.Parallel()
	tests := []struct {
		left, right string
		want        int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.1", "1.0.0", 1},
		{"1.1.0", "1.2.0", -1},
		{"1.0.0-beta.10", "1.0.0-beta.2", 1},
		{"1.0.0-beta.2", "1.0.0", -1},
		{"1.0.0-alpha", "1.0.0-beta", -1},
		{"1.0.0-beta.999999999999999999999999999999", "1.0.0-beta.10", 1},
	}
	for _, test := range tests {
		left, leftErr := parseVersion(test.left)
		right, rightErr := parseVersion(test.right)
		if leftErr != nil || rightErr != nil {
			t.Fatalf("parse: %v %v", leftErr, rightErr)
		}
		got := compareVersions(left, right)
		if got != test.want {
			t.Errorf("compare %s %s = %d, want %d", test.left, test.right, got, test.want)
		}
	}
}

func TestVersionRejectsMalformedInput(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"", "1", "1.2", "01.2.3", "1.2.3-", "1.2.3-beta..1", "1.2.3+build", "1.2.3-01"} {
		if _, err := parseVersion(raw); err == nil {
			t.Errorf("parseVersion(%q) unexpectedly succeeded", raw)
		}
	}
}
