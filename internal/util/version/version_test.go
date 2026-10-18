package version

import "testing"

func TestCompareRejectsUnexpectedFormats(t *testing.T) {
	if _, ok := Compare("latest", "2.9.3"); ok {
		t.Fatal("expected non-semver latest tag to be rejected")
	}
	if _, ok := Compare("v2.9", "2.9.3"); ok {
		t.Fatal("expected short version to be rejected")
	}
}

func TestCompareRejectsInvalidNumbers(t *testing.T) {
	for _, tag := range []string{"v1.02.3", "1.-2.3", "1.+2.3", "1.2.03", "v1.2.3+build", "1.2.3-rc.1"} {
		if _, ok := Compare(tag, "1.2.3"); ok {
			t.Fatalf("accepted invalid stable version %q", tag)
		}
	}
}
