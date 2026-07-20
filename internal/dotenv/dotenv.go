// Package dotenv reads KEY=VALUE pairs from a .env file into the process
// environment, so credentials live in an ignored file instead of shell history.
package dotenv

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// Load reads path and sets each pair into the environment.
//
// Existing environment variables are never overwritten: a value exported in the
// shell or injected by a container platform wins over the file. This keeps .env
// a local-development convenience rather than something that can silently
// override production configuration.
//
// A missing file is not an error — callers are expected to also support plain
// environment variables. Use os.IsNotExist on the returned error to distinguish
// a genuinely unreadable file if that matters.
func Load(path string) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("dotenv: open %s: %w", path, err)
	}
	defer f.Close()

	pairs, err := Parse(f)
	if err != nil {
		return fmt.Errorf("dotenv: %s: %w", path, err)
	}

	for key, value := range pairs {
		if _, set := os.LookupEnv(key); set {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("dotenv: set %s: %w", key, err)
		}
	}
	return nil
}

// Parse reads KEY=VALUE lines and returns them as a map.
//
// It understands blank lines, # comments, an optional "export " prefix, and
// single- or double-quoted values. Escape sequences (\n, \t, \", \\) are
// expanded only inside double quotes, matching common .env conventions.
// Unquoted values have trailing inline comments stripped.
func Parse(r io.Reader) (map[string]string, error) {
	pairs := make(map[string]string)
	scanner := bufio.NewScanner(r)

	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		line = strings.TrimPrefix(line, "export ")

		key, rawValue, found := strings.Cut(line, "=")
		if !found {
			return nil, fmt.Errorf("line %d: missing '=' in %q", lineNo, line)
		}

		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("line %d: empty key", lineNo)
		}

		value, err := parseValue(strings.TrimSpace(rawValue))
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNo, err)
		}
		pairs[key] = value
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return pairs, nil
}

// parseValue unquotes a value and strips inline comments from bare values.
func parseValue(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}

	if quote := raw[0]; quote == '"' || quote == '\'' {
		closing := strings.LastIndexByte(raw, quote)
		if closing == 0 {
			return "", fmt.Errorf("unterminated %c-quoted value", quote)
		}
		value := raw[1:closing]
		// Single quotes are literal; only double quotes expand escapes.
		if quote == '"' {
			value = expandEscapes(value)
		}
		return value, nil
	}

	// A bare value ends at the first " #" — a '#' with no leading space is
	// treated as part of the value, since it appears in generated secrets.
	if idx := strings.Index(raw, " #"); idx >= 0 {
		raw = raw[:idx]
	}
	return strings.TrimSpace(raw), nil
}

// expandEscapes resolves the escape sequences valid inside double quotes.
func expandEscapes(s string) string {
	return strings.NewReplacer(
		`\n`, "\n",
		`\r`, "\r",
		`\t`, "\t",
		`\"`, `"`,
		`\\`, `\`,
	).Replace(s)
}
