package ai

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnthropicRequestAndResponse(t *testing.T) {
	var gotKey, gotVersion, gotBody, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey, gotVersion, gotPath = r.Header.Get("x-api-key"), r.Header.Get("anthropic-version"), r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Write([]byte(`{"content":[{"type":"text","text":"hello "},{"type":"text","text":"world"}]}`))
	}))
	defer srv.Close()

	p, err := New(Config{Kind: "anthropic", Key: "k-123", Model: "m", BaseURL: srv.URL}, nil)
	if err != nil {
		t.Fatal(err)
	}
	out, err := p.Complete(context.Background(), "SYS", "USER")
	if err != nil || out != "hello world" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if gotPath != "/v1/messages" || gotKey != "k-123" || gotVersion == "" ||
		!strings.Contains(gotBody, `"system":"SYS"`) || !strings.Contains(gotBody, `"content":"USER"`) {
		t.Fatalf("bad request: path=%s key=%s body=%s", gotPath, gotKey, gotBody)
	}
}

func TestOpenAICompatibleAndLocalWithoutKey(t *testing.T) {
	var auth, path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth, path = r.Header.Get("Authorization"), r.URL.Path
		w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	}))
	defer srv.Close()

	p, err := New(Config{Kind: "local", Model: "llama", BaseURL: srv.URL + "/v1/"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := p.Complete(context.Background(), "s", "u"); err != nil || out != "ok" {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if auth != "" || path != "/v1/chat/completions" {
		t.Fatalf("auth=%q path=%q", auth, path)
	}
}

func TestProviderErrorDoesNotLeakSecrets(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		w.Write([]byte(`invalid key sk-ant-api03-abcdefghijklmnop`))
	}))
	defer srv.Close()

	p, _ := New(Config{Kind: "anthropic", Key: "k", Model: "m", BaseURL: srv.URL}, nil)
	_, err := p.Complete(context.Background(), "s", "u")
	if err == nil || strings.Contains(err.Error(), "abcdefghijklmnop") || !strings.Contains(err.Error(), "401") {
		t.Fatalf("err=%v", err)
	}
}

func TestConfigValidation(t *testing.T) {
	for name, cfg := range map[string]Config{
		"no model":         {Kind: "openai", Key: "k"},
		"no key anthropic": {Kind: "anthropic", Model: "m"},
		"no key openai":    {Kind: "openai", Model: "m"},
		"unknown kind":     {Kind: "gemini", Key: "k", Model: "m"},
		"local needs url":  {Kind: "local", Model: "m"},
		"plaintext remote": {Kind: "openai", Key: "k", Model: "m", BaseURL: "http://api.example.com/v1"},
		"garbage url":      {Kind: "openai", Key: "k", Model: "m", BaseURL: "::"},
	} {
		if _, err := New(cfg, nil); err == nil {
			t.Errorf("%s was accepted", name)
		}
	}
	if _, err := New(Config{Kind: "openai", Key: "k", Model: "m", BaseURL: "http://127.0.0.1:9/v1"}, nil); err != nil {
		t.Errorf("loopback http should be allowed: %v", err)
	}
}
