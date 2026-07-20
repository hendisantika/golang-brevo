package main

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"golang-brevo/internal/brevo"
)

// envMap turns a map into a getenv func, so precedence tests need no global
// process state and can run in parallel.
func envMap(pairs map[string]string) func(string) string {
	return func(key string) string { return pairs[key] }
}

func noEnv(string) string { return "" }

func TestParseFlags(t *testing.T) {
	t.Parallel()

	cfg, err := parseFlags([]string{
		"-from", "a@example.com",
		"-to", "one@example.com",
		"-to", "two@example.com,three@example.com",
		"-subject", "Hi",
		"-text", "Body",
		"-tag", "onboarding",
		"-tag", "welcome, beta",
	}, io.Discard)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}

	if cfg.from != "a@example.com" {
		t.Errorf("from = %q", cfg.from)
	}
	// -to and -tag accumulate across repeats and split on commas.
	if want := []string{"one@example.com", "two@example.com", "three@example.com"}; !reflect.DeepEqual([]string(cfg.to), want) {
		t.Errorf("to = %v, want %v", cfg.to, want)
	}
	if want := []string{"onboarding", "welcome", "beta"}; !reflect.DeepEqual([]string(cfg.tags), want) {
		t.Errorf("tags = %v, want %v", cfg.tags, want)
	}
}

func TestParseFlagsDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := parseFlags(nil, io.Discard)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if cfg.envFile != defaultEnvFile {
		t.Errorf("envFile = %q, want %q", cfg.envFile, defaultEnvFile)
	}
	if cfg.envFileSet {
		t.Error("envFileSet = true, want false when -env-file is absent")
	}
}

func TestParseFlagsTracksExplicitEnvFile(t *testing.T) {
	t.Parallel()

	// Passing the default value explicitly must still count as explicit.
	cfg, err := parseFlags([]string{"-env-file", defaultEnvFile}, io.Discard)
	if err != nil {
		t.Fatalf("parseFlags: %v", err)
	}
	if !cfg.envFileSet {
		t.Error("envFileSet = false, want true when -env-file is passed")
	}
}

func TestParseFlagsHelpIsNotAnError(t *testing.T) {
	t.Parallel()

	cfg, err := parseFlags([]string{"-h"}, io.Discard)
	if err != nil {
		t.Fatalf("want nil error for -h, got %v", err)
	}
	if cfg != nil {
		t.Error("want nil config for -h")
	}
}

func TestParseFlagsUnknownFlagErrors(t *testing.T) {
	t.Parallel()

	if _, err := parseFlags([]string{"-nope"}, io.Discard); err == nil {
		t.Fatal("want error for unknown flag, got nil")
	}
}

// TestResolveSenderPrecedence is the case that motivated this refactor: the
// flag must beat the environment, which in turn beats nothing at all.
func TestResolveSenderPrecedence(t *testing.T) {
	t.Parallel()

	env := envMap(map[string]string{
		apiKeyEnv:     "key",
		senderEnv:     "env@example.com",
		senderNameEnv: "From Env",
	})

	tests := []struct {
		name         string
		from         string
		fromName     string
		wantEmail    string
		wantNameFrom string
	}{
		{
			name:         "env used when flags are empty",
			wantEmail:    "env@example.com",
			wantNameFrom: "From Env",
		},
		{
			name:         "flag overrides env",
			from:         "flag@example.com",
			fromName:     "From Flag",
			wantEmail:    "flag@example.com",
			wantNameFrom: "From Flag",
		},
		{
			name:         "flags override independently",
			from:         "flag@example.com",
			wantEmail:    "flag@example.com",
			wantNameFrom: "From Env",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := config{from: tt.from, fromName: tt.fromName, to: stringList{"u@example.com"}}
			set, err := resolve(cfg, env)
			if err != nil {
				t.Fatalf("resolve: %v", err)
			}
			if set.email.Sender.Email != tt.wantEmail {
				t.Errorf("sender email = %q, want %q", set.email.Sender.Email, tt.wantEmail)
			}
			if set.email.Sender.Name != tt.wantNameFrom {
				t.Errorf("sender name = %q, want %q", set.email.Sender.Name, tt.wantNameFrom)
			}
		})
	}
}

func TestResolveMissingAPIKey(t *testing.T) {
	t.Parallel()

	_, err := resolve(config{envFile: ".env"}, noEnv)
	if err == nil {
		t.Fatal("want error when api key is absent, got nil")
	}
	// The message should point at the file the user actually configured.
	if !strings.Contains(err.Error(), apiKeyEnv) || !strings.Contains(err.Error(), ".env") {
		t.Errorf("error %q should name %s and the env file", err, apiKeyEnv)
	}
}

func TestResolveNamesCustomEnvFileInError(t *testing.T) {
	t.Parallel()

	_, err := resolve(config{envFile: "secrets/prod.env"}, noEnv)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	if !strings.Contains(err.Error(), "secrets/prod.env") {
		t.Errorf("error %q should name the custom env file", err)
	}
}

func TestResolveBuildsEmail(t *testing.T) {
	t.Parallel()

	cfg := config{
		to:      stringList{"a@example.com", "b@example.com"},
		subject: "Hi",
		text:    "Body",
		html:    "<p>Body</p>",
		tags:    stringList{"welcome", " onboarding ", "welcome"},
	}

	set, err := resolve(cfg, envMap(map[string]string{apiKeyEnv: "key"}))
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}

	if set.apiKey != "key" {
		t.Errorf("apiKey = %q", set.apiKey)
	}
	want := []brevo.Contact{{Email: "a@example.com"}, {Email: "b@example.com"}}
	if !reflect.DeepEqual(set.email.To, want) {
		t.Errorf("to = %v, want %v", set.email.To, want)
	}
	// Tags are normalized on the way through.
	if want := []string{"welcome", "onboarding"}; !reflect.DeepEqual(set.email.Tags, want) {
		t.Errorf("tags = %v, want %v", set.email.Tags, want)
	}
	if set.email.Subject != "Hi" || set.email.TextContent != "Body" || set.email.HTMLContent != "<p>Body</p>" {
		t.Errorf("content not carried through: %+v", set.email)
	}
}

func TestLoadEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("MAIN_TEST_KEY=value\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("MAIN_TEST_KEY", "")
	os.Unsetenv("MAIN_TEST_KEY")

	if err := loadEnvFile(config{envFile: path, envFileSet: true}); err != nil {
		t.Fatalf("loadEnvFile: %v", err)
	}
	if got := os.Getenv("MAIN_TEST_KEY"); got != "value" {
		t.Errorf("got %q, want %q", got, "value")
	}
}

func TestLoadEnvFileMissing(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "absent.env")

	// Implicit default: absence is fine, plain env vars may still be set.
	if err := loadEnvFile(config{envFile: missing}); err != nil {
		t.Errorf("implicit missing file should not error, got %v", err)
	}
	// Explicit request: absence is a typo worth reporting.
	if err := loadEnvFile(config{envFile: missing, envFileSet: true}); err == nil {
		t.Error("explicit missing file should error, got nil")
	}
}

func TestReport(t *testing.T) {
	t.Parallel()

	email := brevo.Email{To: []brevo.Contact{{Email: "a@example.com"}}}
	email = email.WithTags("welcome")

	var buf strings.Builder
	report(&buf, email, &brevo.SendResult{MessageID: "<id@brevo>"})

	for _, want := range []string{"a@example.com", "<id@brevo>", "welcome"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("output missing %q:\n%s", want, buf.String())
		}
	}
}

func TestReportWithoutMessageID(t *testing.T) {
	t.Parallel()

	email := brevo.Email{To: []brevo.Contact{{Email: "a@example.com"}}}

	var buf strings.Builder
	report(&buf, email, &brevo.SendResult{})

	// A missing id is reported, not treated as a failure.
	if !strings.Contains(buf.String(), "none returned") {
		t.Errorf("want a note about the missing id:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "tags:") {
		t.Errorf("untagged email should print no tags line:\n%s", buf.String())
	}
}
