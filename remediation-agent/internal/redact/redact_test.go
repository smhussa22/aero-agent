package redact

import (
	"strings"
	"testing"
)

func TestRedact(t *testing.T) {
	for name, secret := range map[string]string{
		"anthropic key": "sk-ant-api03-abcdefghijklmnop",
		"openai key":    "sk-proj1234567890abcdefgh",
		"aws key":       "AKIAIOSFODNN7EXAMPLE",
		"github token":  "ghp_abcdefghijklmnopqrstuvwxyz0123",
		"jwt":           "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N",
		"bearer":        "Bearer abcdef0123456789abcdef",
		"url password":  "hunter2hunter2",
		"password=":     "s3cretValue",
	} {
		in := "log line " + secret + " end"
		switch name {
		case "url password":
			in = "connecting to postgres://admin:" + secret + "@db:5432/app"
		case "password=":
			in = "starting with password=" + secret + " port=80"
		}
		if out := Redact(in); strings.Contains(out, secret) {
			t.Errorf("%s leaked: %q", name, out)
		}
	}
}

func TestRedactPrivateKeyBlock(t *testing.T) {
	in := "x\n-----BEGIN RSA PRIVATE KEY-----\nMIIEow\nabc\n-----END RSA PRIVATE KEY-----\ny"
	if out := Redact(in); strings.Contains(out, "MIIEow") || !strings.Contains(out, "x\n") || !strings.HasSuffix(out, "\ny") {
		t.Fatalf("got %q", out)
	}
}

func TestRedactLeavesOrdinaryTextAlone(t *testing.T) {
	in := "nginx[812]: worker process exited with code 1; cpu 96.4%; user=www-data"
	if out := Redact(in); out != in {
		t.Fatalf("changed ordinary text: %q", out)
	}
}
