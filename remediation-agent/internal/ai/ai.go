// Package ai talks to the user's own AI provider with the user's own key.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/smhussa22/aero-agent/remediation-agent/internal/redact"
)

const (
	maxResponseBytes = 1 << 20
	maxTokens        = 1024
)

// Provider returns the model's text reply to one system + user prompt.
type Provider interface {
	Complete(ctx context.Context, system, user string) (string, error)
}

// Config selects and configures a provider. Kind is anthropic, openai or local
// (any OpenAI-compatible server such as Ollama or llama.cpp).
type Config struct {
	Kind    string
	Key     string
	Model   string
	BaseURL string
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// New validates cfg and returns the matching provider.
func New(cfg Config, client *http.Client) (Provider, error) {
	if cfg.Model == "" {
		return nil, errors.New("a model name is required")
	}
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}

	base := cfg.BaseURL
	switch cfg.Kind {
	case "anthropic":
		if cfg.Key == "" {
			return nil, errors.New("an API key is required for anthropic")
		}
		if base == "" {
			base = "https://api.anthropic.com"
		}
	case "openai":
		if cfg.Key == "" {
			return nil, errors.New("an API key is required for openai")
		}
		if base == "" {
			base = "https://api.openai.com/v1"
		}
	case "local":
		if base == "" {
			return nil, errors.New("a base URL is required for a local model")
		}
	default:
		return nil, fmt.Errorf("unknown provider %q (use anthropic, openai or local)", cfg.Kind)
	}

	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return nil, errors.New("invalid base URL")
	}
	// never send a key over plaintext to a remote host
	if u.Scheme != "https" && cfg.Kind != "local" && !isLoopback(u.Hostname()) {
		return nil, errors.New("base URL must be https for a hosted provider")
	}
	base = strings.TrimRight(base, "/")

	if cfg.Kind == "anthropic" {
		return &anthropic{cfg: cfg, base: base, client: client}, nil
	}
	return &openAI{cfg: cfg, base: base, client: client}, nil
}

// post sends a JSON request and returns the (size-limited) body of a 2xx reply.
func post(ctx context.Context, client *http.Client, url string, headers map[string]string, payload any) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		msg := redact.Redact(string(data))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return nil, fmt.Errorf("provider returned %d: %s", resp.StatusCode, msg)
	}
	return data, nil
}

type anthropic struct {
	cfg    Config
	base   string
	client *http.Client
}

func (a *anthropic) Complete(ctx context.Context, system, user string) (string, error) {
	data, err := post(ctx, a.client, a.base+"/v1/messages",
		map[string]string{"x-api-key": a.cfg.Key, "anthropic-version": "2023-06-01"},
		map[string]any{
			"model": a.cfg.Model, "max_tokens": maxTokens, "system": system,
			"messages": []map[string]string{{"role": "user", "content": user}},
		})
	if err != nil {
		return "", err
	}

	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", errors.New("unreadable provider response")
	}
	var text strings.Builder
	for _, c := range out.Content {
		if c.Type == "text" {
			text.WriteString(c.Text)
		}
	}
	if text.Len() == 0 {
		return "", errors.New("provider returned no text")
	}
	return text.String(), nil
}

type openAI struct {
	cfg    Config
	base   string
	client *http.Client
}

func (o *openAI) Complete(ctx context.Context, system, user string) (string, error) {
	headers := map[string]string{}
	if o.cfg.Key != "" {
		headers["Authorization"] = "Bearer " + o.cfg.Key
	}
	data, err := post(ctx, o.client, o.base+"/chat/completions", headers, map[string]any{
		"model": o.cfg.Model, "temperature": 0, "max_tokens": maxTokens,
		"messages": []map[string]string{{"role": "system", "content": system}, {"role": "user", "content": user}},
	})
	if err != nil {
		return "", err
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &out); err != nil || len(out.Choices) == 0 || out.Choices[0].Message.Content == "" {
		return "", errors.New("unreadable or empty provider response")
	}
	return out.Choices[0].Message.Content, nil
}
