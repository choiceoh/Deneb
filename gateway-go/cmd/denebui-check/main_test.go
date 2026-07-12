package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestCheckValidatesEveryFence(t *testing.T) {
	input := "before\n```deneb-ui\n<text>one</text>\n```\nafter\n```DENEB-UI\n<alert severity=\"info\">two</alert>\n```"
	var stdout, stderr bytes.Buffer
	if code := check(input, false, &stdout, &stderr); code != 0 {
		t.Fatalf("check code = %d, output=%s", code, stdout.String())
	}
	if strings.Count(stdout.String(), "VALID") != 2 {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestCheckRawAndInvalidInputs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := check(`<text>raw</text>`, false, &stdout, &stderr); code != 0 {
		t.Fatalf("raw check code = %d", code)
	}
	if !strings.Contains(stderr.String(), "no ```deneb-ui fence") {
		t.Fatalf("stderr = %q", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if code := check(`{not json`, false, &stdout, &stderr); code != 1 {
		t.Fatalf("invalid check code = %d", code)
	}
	if !strings.Contains(stdout.String(), "NOT PARSEABLE") {
		t.Fatalf("stdout = %q", stdout.String())
	}
}
