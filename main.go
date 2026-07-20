// Command golang-brevo sends a tagged transactional email through Brevo.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang-brevo/internal/brevo"
	"golang-brevo/internal/dotenv"
)

// Credentials are read from the environment — populated either by the shell or
// by a gitignored .env file — so they never land in shell history or a
// committed flag default.
const (
	apiKeyEnv     = "BREVO_API_KEY"
	senderEnv     = "BREVO_SENDER_EMAIL"
	senderNameEnv = "BREVO_SENDER_NAME"
)

// defaultEnvFile is loaded when -env-file is not given. Missing is fine.
const defaultEnvFile = ".env"

// sendTimeout bounds a single send, on top of the client's own timeout.
const sendTimeout = 30 * time.Second

// config is the raw command line, before the environment is consulted.
type config struct {
	envFile string
	// envFileSet records whether -env-file was passed explicitly, which
	// decides if a missing file is an error.
	envFileSet bool

	from     string
	fromName string
	to       stringList
	subject  string
	text     string
	html     string
	tags     stringList
}

// settings is a fully resolved run: flags merged over the environment.
type settings struct {
	apiKey string
	email  brevo.Email
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
	if err := run(os.Args[1:], os.Stderr, os.Stdout); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// run wires the pieces together: parse, load .env, resolve, send.
func run(args []string, stderr, stdout io.Writer) error {
	cfg, err := parseFlags(args, stderr)
	if err != nil {
		return err
	}
	if cfg == nil {
		return nil // -h: usage already printed
	}

	if err := loadEnvFile(*cfg); err != nil {
		return err
	}

	set, err := resolve(*cfg, os.Getenv)
	if err != nil {
		return err
	}

	client, err := brevo.New(set.apiKey)
	if err != nil {
		return err
	}

	// Cancel in-flight requests on Ctrl-C rather than leaving them hanging.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()

	result, err := client.SendEmail(ctx, set.email)
	if err != nil {
		return err
	}

	report(stdout, set.email, result)
	return nil
}

// parseFlags builds a config from args. It returns (nil, nil) when the user
// asked for help, which is a successful exit rather than an error.
func parseFlags(args []string, out io.Writer) (*config, error) {
	var cfg config

	fs := flag.NewFlagSet("golang-brevo", flag.ContinueOnError)
	fs.SetOutput(out)
	fs.StringVar(&cfg.envFile, "env-file", defaultEnvFile, "path to a .env file holding credentials")
	fs.StringVar(&cfg.from, "from", "", "sender email address (defaults to $"+senderEnv+")")
	fs.StringVar(&cfg.fromName, "from-name", "", "sender display name (defaults to $"+senderNameEnv+")")
	fs.Var(&cfg.to, "to", "recipient email; repeatable or comma-separated (required)")
	fs.StringVar(&cfg.subject, "subject", "", "email subject (required)")
	fs.StringVar(&cfg.text, "text", "", "plain-text body")
	fs.StringVar(&cfg.html, "html", "", "HTML body")
	fs.Var(&cfg.tags, "tag", "tag to attach; repeatable or comma-separated")
	fs.Usage = func() { usage(fs) }

	if err := fs.Parse(args); err != nil {
		// Parse already reported the problem and printed usage.
		if errors.Is(err, flag.ErrHelp) {
			return nil, nil
		}
		return nil, err
	}

	fs.Visit(func(f *flag.Flag) {
		if f.Name == "env-file" {
			cfg.envFileSet = true
		}
	})
	return &cfg, nil
}

func usage(fs *flag.FlagSet) {
	out := fs.Output()
	fmt.Fprintf(out, "Send a tagged transactional email via the Brevo API.\n\n")
	fmt.Fprintf(out, "Usage:\n  golang-brevo [flags]\n\nFlags:\n")
	fs.PrintDefaults()
	fmt.Fprintf(out, "\nCredentials are read from %s, %s, and %s.\n", apiKeyEnv, senderEnv, senderNameEnv)
	fmt.Fprintf(out, "These come from a %s file in the working directory, or from the\n", defaultEnvFile)
	fmt.Fprintf(out, "shell environment, which takes precedence. See .env.example.\n")
	fmt.Fprintf(out, "\nExample .env:\n"+
		"  %s=xkeysib-...\n"+
		"  %s=no-reply@example.com\n"+
		"  %s=Example\n", apiKeyEnv, senderEnv, senderNameEnv)
	fmt.Fprintf(out, "\nExample:\n"+
		"  golang-brevo \\\n"+
		"    -to user@example.com \\\n"+
		"    -subject \"Welcome aboard\" \\\n"+
		"    -text \"Thanks for signing up.\" \\\n"+
		"    -tag onboarding -tag welcome\n")
}

// loadEnvFile populates the environment from cfg.envFile. A missing default
// file is fine, but a file the user named explicitly must exist — silently
// ignoring a typo'd path would fall back to the wrong credentials.
func loadEnvFile(cfg config) error {
	if cfg.envFileSet {
		if _, err := os.Stat(cfg.envFile); err != nil {
			return fmt.Errorf("env file %s: %w", cfg.envFile, err)
		}
	}
	return dotenv.Load(cfg.envFile)
}

// resolve merges the command line over the environment and builds the message.
// getenv is injected so tests can exercise precedence without touching global
// process state.
func resolve(cfg config, getenv func(string) string) (settings, error) {
	apiKey := getenv(apiKeyEnv)
	if apiKey == "" {
		return settings{}, fmt.Errorf(
			"%s is not set; add it to %s or export it (see .env.example)", apiKeyEnv, cfg.envFile)
	}

	// Flags win over the environment, so a .env sender can be overridden per run.
	from := cfg.from
	if from == "" {
		from = getenv(senderEnv)
	}
	fromName := cfg.fromName
	if fromName == "" {
		fromName = getenv(senderNameEnv)
	}

	email := brevo.Email{
		Sender:      brevo.Contact{Email: from, Name: fromName},
		Subject:     cfg.subject,
		TextContent: cfg.text,
		HTMLContent: cfg.html,
	}
	for _, addr := range cfg.to {
		email.To = append(email.To, brevo.Contact{Email: addr})
	}

	return settings{apiKey: apiKey, email: email.WithTags(cfg.tags...)}, nil
}

// report prints a human-readable summary of an accepted send.
func report(out io.Writer, email brevo.Email, result *brevo.SendResult) {
	recipients := make([]string, 0, len(email.To))
	for _, c := range email.To {
		recipients = append(recipients, c.Email)
	}
	fmt.Fprintf(out, "sent to %s\n", strings.Join(recipients, ", "))

	id, err := result.ID()
	if err != nil {
		// Brevo accepted the message; a missing id is not a send failure.
		fmt.Fprintf(out, "message id: (none returned)\n")
	} else {
		fmt.Fprintf(out, "message id: %s\n", id)
	}

	if len(email.Tags) > 0 {
		fmt.Fprintf(out, "tags: %s\n", strings.Join(email.Tags, ", "))
	}
}
