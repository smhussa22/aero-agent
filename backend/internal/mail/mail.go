// Package mail sends transactional email through an email API (Resend). We run no mail server and speak no SMTP.
package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Sender sends one plain-text message.
type Sender interface {
	Send(ctx context.Context, to, subject, text string) error
}

// Resend sends through https://resend.com. The base URL is a field so tests can point it at a fake.
type Resend struct {
	APIKey  string
	From    string // for example "Aero Agent <noreply@your-domain>"; the domain must be verified with Resend
	BaseURL string
	Client  *http.Client
}

func (r Resend) Send(ctx context.Context, to, subject, text string) error {
	if r.APIKey == "" || r.From == "" {
		return errors.New("email is not configured")
	}
	base := r.BaseURL
	if base == "" {
		base = "https://api.resend.com"
	}
	client := r.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	body, _ := json.Marshal(map[string]any{"from": r.From, "to": []string{to}, "subject": subject, "text": text})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/emails", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+r.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("email request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		detail, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
		return fmt.Errorf("email provider returned %d: %s", resp.StatusCode, detail)
	}
	return nil
}
