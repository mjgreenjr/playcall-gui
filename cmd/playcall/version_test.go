package main

import (
	"bytes"
	"context"
	"testing"

	"playcall/internal/version"
)

func TestVersionFlagPrintsAndExits(t *testing.T) {
	var out, errOut bytes.Buffer
	// No source, paths or clock: --version must not need any of them.
	code := run(context.Background(), deps{Stdout: &out, Stderr: &errOut}, []string{"--version"})
	if code != exitOK || out.String() != "playcall "+version.Version+"\n" || errOut.Len() != 0 {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out.String(), errOut.String())
	}
}
