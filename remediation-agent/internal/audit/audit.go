// Package audit is the agent's local, append-only, tamper-evident record of every decision.
// Each line carries the hash of the previous line, so editing or deleting history is detectable.
package audit

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"
)

type Event struct {
	Time       time.Time `json:"time"`
	IncidentID string    `json:"incident_id"`
	ActionID   string    `json:"action_id"`
	Event      string    `json:"event"` // proposed, approved, running, verified, ...
	Actor      string    `json:"actor"` // who or what caused it
	Detail     string    `json:"detail"`
	DryRun     bool      `json:"dry_run"`
}

// Sink receives audit events. Record must not return nil unless the event is durably stored.
type Sink interface {
	Record(ctx context.Context, e Event) error
}

type entry struct {
	Event Event  `json:"event"`
	Prev  string `json:"prev"`
	Hash  string `json:"hash"`
}

func chain(prev string, e Event) (string, error) {
	b, err := json.Marshal(e)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(append([]byte(prev), b...))
	return hex.EncodeToString(sum[:]), nil
}

// FileSink appends hash-chained JSON lines to a file.
type FileSink struct {
	mu   sync.Mutex
	f    *os.File
	prev string
}

// OpenFile opens (creating if needed) the log and continues its chain. It refuses a log
// whose existing chain does not verify, rather than appending to tampered history.
func OpenFile(path string) (*FileSink, error) {
	last, err := verify(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	return &FileSink{f: f, prev: last}, nil
}

func (s *FileSink) Record(_ context.Context, e Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	hash, err := chain(s.prev, e)
	if err != nil {
		return err
	}
	line, err := json.Marshal(entry{Event: e, Prev: s.prev, Hash: hash})
	if err != nil {
		return err
	}
	if _, err := s.f.Write(append(line, '\n')); err != nil {
		return err
	}
	if err := s.f.Sync(); err != nil { // durable before the caller proceeds
		return err
	}
	s.prev = hash
	return nil
}

func (s *FileSink) Close() error { return s.f.Close() }

// verify walks the file and returns the hash of the last entry.
func verify(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	prev := ""
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	for n := 1; sc.Scan(); n++ {
		var e entry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			return "", fmt.Errorf("audit line %d is corrupt: %w", n, err)
		}
		want, err := chain(prev, e.Event)
		if err != nil {
			return "", err
		}
		if e.Prev != prev || e.Hash != want {
			return "", fmt.Errorf("audit line %d does not match the chain: log was modified", n)
		}
		prev = e.Hash
	}
	return prev, sc.Err()
}

// Verify checks the whole log and returns an error describing the first broken line.
func Verify(path string) error {
	_, err := verify(path)
	return err
}
