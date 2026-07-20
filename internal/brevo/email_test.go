package brevo

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

// newTestClient wires a Client to a stub server and returns both.
func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	c, err := New("test-key", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return c
}

func validEmail() Email {
	return Email{
		Sender:      Contact{Email: "no-reply@example.com", Name: "Example"},
		To:          []Contact{{Email: "user@example.com"}},
		Subject:     "Hello",
		TextContent: "Hi there",
	}
}

func TestNewRequiresAPIKey(t *testing.T) {
	if _, err := New(""); !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("want ErrMissingAPIKey, got %v", err)
	}
}

func TestSendEmailSendsAPIKeyAndTags(t *testing.T) {
	var gotKey, gotPath string
	var gotBody Email

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("api-key")
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"messageId": "<abc@brevo>"})
	})

	email := validEmail().WithTags("onboarding", "welcome")

	res, err := c.SendEmail(context.Background(), email)
	if err != nil {
		t.Fatalf("SendEmail: %v", err)
	}

	if gotKey != "test-key" {
		t.Errorf("api-key header = %q, want %q", gotKey, "test-key")
	}
	if gotPath != "/smtp/email" {
		t.Errorf("path = %q, want %q", gotPath, "/smtp/email")
	}
	if want := []string{"onboarding", "welcome"}; !reflect.DeepEqual(gotBody.Tags, want) {
		t.Errorf("tags = %v, want %v", gotBody.Tags, want)
	}

	id, err := res.ID()
	if err != nil {
		t.Fatalf("ID: %v", err)
	}
	if id != "<abc@brevo>" {
		t.Errorf("id = %q, want %q", id, "<abc@brevo>")
	}
}

func TestWithTagsNormalizesAndDoesNotMutateReceiver(t *testing.T) {
	base := validEmail().WithTags("welcome")

	tagged := base.WithTags("  onboarding  ", "", "welcome", "beta")

	if want := []string{"welcome", "onboarding", "beta"}; !reflect.DeepEqual(tagged.Tags, want) {
		t.Errorf("tags = %v, want %v", tagged.Tags, want)
	}
	if want := []string{"welcome"}; !reflect.DeepEqual(base.Tags, want) {
		t.Errorf("receiver mutated: base tags = %v, want %v", base.Tags, want)
	}
}

func TestSendEmailOmitsEmptyTags(t *testing.T) {
	var raw map[string]any

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&raw)
		json.NewEncoder(w).Encode(map[string]string{"messageId": "x"})
	})

	// Tags that normalize away entirely must not reach the wire.
	email := validEmail()
	email.Tags = []string{"", "   "}

	if _, err := c.SendEmail(context.Background(), email); err != nil {
		t.Fatalf("SendEmail: %v", err)
	}
	if _, present := raw["tags"]; present {
		t.Errorf("tags key present in payload, want omitted")
	}
}

func TestSendEmailValidationFailsBeforeRequest(t *testing.T) {
	called := false
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	tests := map[string]func(*Email){
		"no sender":     func(e *Email) { e.Sender.Email = "" },
		"no recipients": func(e *Email) { e.To = nil },
		"no subject":    func(e *Email) { e.Subject = "" },
		"no body":       func(e *Email) { e.TextContent = ""; e.HTMLContent = "" },
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			email := validEmail()
			mutate(&email)
			if _, err := c.SendEmail(context.Background(), email); err == nil {
				t.Fatal("want validation error, got nil")
			}
		})
	}

	if called {
		t.Error("server was called despite invalid input")
	}
}

func TestSendEmailTemplateNeedsNoSubjectOrBody(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"messageId": "x"})
	})

	email := Email{
		Sender:     Contact{Email: "no-reply@example.com"},
		To:         []Contact{{Email: "user@example.com"}},
		TemplateID: 42,
	}

	if _, err := c.SendEmail(context.Background(), email); err != nil {
		t.Fatalf("SendEmail: %v", err)
	}
}

func TestSendEmailSurfacesAPIError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{
			"code":    "unauthorized",
			"message": "Key not found",
		})
	})

	_, err := c.SendEmail(context.Background(), validEmail())

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want *APIError, got %v", err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", apiErr.StatusCode, http.StatusUnauthorized)
	}
	if apiErr.Code != "unauthorized" {
		t.Errorf("code = %q, want %q", apiErr.Code, "unauthorized")
	}
}

func TestSendEmailNonJSONErrorBody(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte("upstream exploded"))
	})

	_, err := c.SendEmail(context.Background(), validEmail())

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("want *APIError, got %v", err)
	}
	if apiErr.Message != "upstream exploded" {
		t.Errorf("message = %q, want %q", apiErr.Message, "upstream exploded")
	}
}

func TestResultIDFallsBackToMessageIDs(t *testing.T) {
	r := &SendResult{MessageIDs: []string{"first", "second"}}
	id, err := r.ID()
	if err != nil {
		t.Fatalf("ID: %v", err)
	}
	if id != "first" {
		t.Errorf("id = %q, want %q", id, "first")
	}

	if _, err := (&SendResult{}).ID(); !errors.Is(err, ErrNoMessageID) {
		t.Errorf("want ErrNoMessageID, got %v", err)
	}
}
