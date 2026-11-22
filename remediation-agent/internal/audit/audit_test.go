package audit

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func ev(name string) Event {
	return Event{Time: time.Unix(1_700_000_000, 0).UTC(), IncidentID: "i1", ActionID: "restart_service", Event: name, Actor: "agent"}
}

func writeThree(t *testing.T, path string) {
	t.Helper()
	s, err := OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"proposed", "approved", "running"} {
		if err := s.Record(context.Background(), ev(n)); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
}

func TestChainVerifiesAndContinuesAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	writeThree(t, path)
	if err := Verify(path); err != nil {
		t.Fatal(err)
	}

	// reopening must continue the same chain, not start a new one
	s, err := OpenFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s.Record(context.Background(), ev("verified"))
	s.Close()
	if err := Verify(path); err != nil {
		t.Fatalf("chain broke after reopen: %v", err)
	}
}

func TestTamperingIsDetected(t *testing.T) {
	for name, mutate := range map[string]func(string) string{
		"edited field": func(s string) string { return strings.Replace(s, `"approved"`, `"rejected"`, 1) },
		"deleted line": func(s string) string { l := strings.SplitN(s, "\n", 3); return l[0] + "\n" + l[2] },
		"reordered": func(s string) string {
			l := strings.Split(strings.TrimRight(s, "\n"), "\n")
			l[0], l[1] = l[1], l[0]
			return strings.Join(l, "\n") + "\n"
		},
	} {
		path := filepath.Join(t.TempDir(), "audit.log")
		writeThree(t, path)
		b, _ := os.ReadFile(path)
		os.WriteFile(path, []byte(mutate(string(b))), 0o600)

		if Verify(path) == nil {
			t.Errorf("%s was not detected", name)
		}
		if _, err := OpenFile(path); err == nil {
			t.Errorf("%s: OpenFile appended to a tampered log", name)
		}
	}
}

func TestLogIsPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.log")
	writeThree(t, path)
	if info, _ := os.Stat(path); info.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Errorf("log is readable by others: %v", info.Mode())
	}
}
