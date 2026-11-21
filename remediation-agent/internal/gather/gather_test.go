package gather

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeRunner map[string]struct {
	out string
	err error
}

func (f fakeRunner) Run(_ context.Context, name string, _ ...string) (string, error) {
	r := f[name]
	return r.out, r.err
}

func TestGatherRedactsClipsAndSurvivesFailures(t *testing.T) {
	long := strings.Repeat("line\n", 100)
	g := Gatherer{Timeout: time.Second, Runner: fakeRunner{
		"ps":         {out: "1 root nginx 99 1\nDB_PASSWORD=hunter2 leaked\n"},
		"journalctl": {out: long},
		"systemctl":  {err: errors.New("exec: not found")},
	}}

	ctx := g.Gather(context.Background())
	if len(ctx.Sections) != len(commands) {
		t.Fatalf("sections=%d", len(ctx.Sections))
	}
	by := map[string]string{}
	for _, s := range ctx.Sections {
		by[s.Title] = s.Body
	}

	if strings.Contains(by["Top processes by CPU"], "hunter2") {
		t.Error("secret was not redacted")
	}
	if !strings.Contains(by["Recent warnings and errors"], "more lines omitted") {
		t.Error("long output was not clipped")
	}
	if !strings.HasPrefix(by["Failed units"], "unavailable:") {
		t.Errorf("failed command should become a note, got %q", by["Failed units"])
	}
}

func TestLimitedBufferCapsOutput(t *testing.T) {
	l := &limitedBuffer{max: 5}
	n, _ := l.Write([]byte("0123456789"))
	if n != 10 || l.b.String() != "01234" {
		t.Fatalf("n=%d got=%q", n, l.b.String())
	}
}
