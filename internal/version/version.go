// Package version holds the one place playcall's version number is written.
// Everything that shows it (both binaries' --version, the GUI's settings
// screen) reads it from here, and a test checks that CHANGELOG.md's newest
// release is the same number.
package version

// Version is the semantic version of the current release; changes since it
// are listed under "Unreleased" in CHANGELOG.md until a release is cut.
const Version = "0.4.0"
