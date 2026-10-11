package version

import (
	"os"
	"regexp"
	"testing"
)

func TestVersionIsSemver(t *testing.T) {
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(Version) {
		t.Errorf("Version %q is not MAJOR.MINOR.PATCH", Version)
	}
}

// The changelog's newest release heading must be the version in the code, so
// the two cannot drift apart.
func TestChangelogNewestReleaseIsVersion(t *testing.T) {
	data, err := os.ReadFile("../../CHANGELOG.md")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?m)^## \[(\d+\.\d+\.\d+)\]`).FindSubmatch(data)
	if m == nil {
		t.Fatal("CHANGELOG.md has no '## [x.y.z]' release heading")
	}
	if string(m[1]) != Version {
		t.Errorf("CHANGELOG.md's newest release is %s, Version is %s", m[1], Version)
	}
}
