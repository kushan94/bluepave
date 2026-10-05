package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestValidateExample(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"validate", "-f", "../../examples/bluepave.yaml", "-root", "../.."}, &out, &errOut)
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), `platform "acme-platform", profile trial`) {
		t.Errorf("stdout = %q", out.String())
	}
}

func TestUnknownCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run([]string{"deploy"}, &out, &errOut); code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
}
