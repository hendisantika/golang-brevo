// Command golang-brevo sends a tagged transactional email through Brevo.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang-brevo/internal/brevo"
)

// apiKeyEnv holds the Brevo credential. It is read from the environment so the
// key never lands in shell history or a committed flag default.
const apiKeyEnv = "BREVO_API_KEY"

type config struct {
	from     string
	fromName string
	to       stringList
	subject  string
	text     string
	html     string
	tags     stringList
}

// stringList collects a repeatable flag into a slice.
type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }

func (s *stringList) Set(v string) error {
	// Accept both -tag a -tag b and -tag a,b.
	for _, part := range strings.Split(v, ",") {
		if part = strings.TrimSpace(part); part != "" {
			*s = append(*s, part)
		}
	}
	return nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	var cfg config

	fs := flag.NewFlagSet("golang-brevo", flag.ContinueOnError)
	fs.StringVar(&cfg.from, "from", "", "sender email address (required)")
	fs.StringVar(&cfg.fromName, "from-name", "", "sender display name")
	fs.Var(&cfg.to, "to", "recipient email; repeatable or comma-separated (required)")
	fs.StringVar(&cfg.subject, "subject", "", "email subject (required)")
	fs.StringVar(&cfg.text, "text", "", "plain-text body")
	fs.StringVar(&cfg.html, "html", "", "HTML body")
	fs.Var(&cfg.tags, "tag", "tag to attach; repeatable or comma-separated")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Send a tagged transactional email via the Brevo API.\n\n")
		fmt.Fprintf(fs.Output(), "Usage:\n  golang-brevo [flags]\n\nFlags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(fs.Output(), "\nThe %s environment variable must be set.\n", apiKeyEnv)
		fmt.Fprintf(fs.Output(), "\nExample:\n"+
			"  export %s=xkeysib-...\n"+
			"  golang-brevo \\\n"+
			"    -from no-reply@example.com -from-name \"Example\" \\\n"+
			"    -to user@example.com \\\n"+
			"    -subject \"Welcome aboard\" \\\n"+
			"    -text \"Thanks for signing up.\" \\\n"+
			"    -tag onboarding -tag welcome\n", apiKeyEnv)
	}

	if err := fs.Parse(args); err != nil {
		// Parse already reported the problem and printed usage.
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}

	apiKey := os.Getenv(apiKeyEnv)
	if apiKey == "" {
		return fmt.Errorf("%s is not set; export your Brevo API key first", apiKeyEnv)
	}

	client, err := brevo.New(apiKey)
	if err != nil {
		return err
	}

	email := brevo.Email{
		Sender:      brevo.Contact{Email: cfg.from, Name: cfg.fromName},
		Subject:     cfg.subject,
		TextContent: cfg.text,
		HTMLContent: cfg.html,
	}
	for _, addr := range cfg.to {
		email.To = append(email.To, brevo.Contact{Email: addr})
	}
	email = email.WithTags(cfg.tags...)

	// Cancel in-flight requests on Ctrl-C rather than leaving them hanging.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	result, err := client.SendEmail(ctx, email)
	if err != nil {
		return err
	}

	id, err := result.ID()
	if err != nil {
		// Brevo accepted the message; a missing id is not a send failure.
		fmt.Printf("sent to %s (no message id returned)\n", strings.Join(cfg.to, ", "))
		return nil
	}

	fmt.Printf("sent to %s\n", strings.Join(cfg.to, ", "))
	fmt.Printf("message id: %s\n", id)
	if len(email.Tags) > 0 {
		fmt.Printf("tags: %s\n", strings.Join(email.Tags, ", "))
	}
	return nil
}
