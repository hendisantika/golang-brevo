package brevo

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Contact is a sender or recipient address.
type Contact struct {
	Email string `json:"email"`
	Name  string `json:"name,omitempty"`
}

// Attachment is a file sent alongside the email. Provide either URL or
// Content (base64-encoded) — Brevo requires Name when Content is used.
type Attachment struct {
	URL     string `json:"url,omitempty"`
	Content string `json:"content,omitempty"`
	Name    string `json:"name,omitempty"`
}

// Email is a transactional message for POST /v3/smtp/email.
type Email struct {
	Sender      Contact           `json:"sender"`
	To          []Contact         `json:"to"`
	CC          []Contact         `json:"cc,omitempty"`
	BCC         []Contact         `json:"bcc,omitempty"`
	ReplyTo     *Contact          `json:"replyTo,omitempty"`
	Subject     string            `json:"subject,omitempty"`
	HTMLContent string            `json:"htmlContent,omitempty"`
	TextContent string            `json:"textContent,omitempty"`
	TemplateID  int64             `json:"templateId,omitempty"`
	Params      map[string]any    `json:"params,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`

	// Tags label the message so it can be filtered in the Brevo dashboard
	// and matched in webhook events. They are never shown to the recipient.
	Tags []string `json:"tags,omitempty"`
}

// SendResult holds the identifiers Brevo assigns to an accepted message.
type SendResult struct {
	MessageID  string   `json:"messageId"`
	MessageIDs []string `json:"messageIds"`
}

// WithTags returns a copy of the email carrying the given tags, normalized
// (trimmed, empties dropped, duplicates removed) and appended to any existing
// tags. The receiver is left untouched so a base email can be reused.
func (e Email) WithTags(tags ...string) Email {
	e.Tags = normalizeTags(append(append([]string(nil), e.Tags...), tags...))
	return e
}

// normalizeTags trims whitespace, drops empty entries, and removes duplicates
// while preserving first-seen order.
func normalizeTags(tags []string) []string {
	if len(tags) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(tags))
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		if _, dup := seen[tag]; dup {
			continue
		}
		seen[tag] = struct{}{}
		out = append(out, tag)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// Validate reports whether the email has the fields Brevo requires.
func (e Email) Validate() error {
	var problems []string

	if e.Sender.Email == "" {
		problems = append(problems, "sender.email is required")
	}
	if len(e.To) == 0 {
		problems = append(problems, "at least one recipient in to is required")
	}
	for i, rcpt := range e.To {
		if rcpt.Email == "" {
			problems = append(problems, fmt.Sprintf("to[%d].email is required", i))
		}
	}

	// A message is either template-driven or carries its own subject + body.
	if e.TemplateID == 0 {
		if e.Subject == "" {
			problems = append(problems, "subject is required when templateId is not set")
		}
		if e.HTMLContent == "" && e.TextContent == "" {
			problems = append(problems, "htmlContent or textContent is required when templateId is not set")
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("brevo: invalid email: %s", strings.Join(problems, "; "))
	}
	return nil
}

// SendEmail delivers a transactional email. Tags are normalized before the
// request so callers can pass raw user input.
func (c *Client) SendEmail(ctx context.Context, email Email) (*SendResult, error) {
	email.Tags = normalizeTags(email.Tags)

	if err := email.Validate(); err != nil {
		return nil, err
	}

	var result SendResult
	if err := c.post(ctx, "/smtp/email", email, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ErrNoMessageID is returned by SendResult.ID when Brevo accepted the request
// but reported no identifier.
var ErrNoMessageID = errors.New("brevo: response contained no message id")

// ID returns the primary message identifier for the send.
func (r *SendResult) ID() (string, error) {
	if r.MessageID != "" {
		return r.MessageID, nil
	}
	if len(r.MessageIDs) > 0 {
		return r.MessageIDs[0], nil
	}
	return "", ErrNoMessageID
}
