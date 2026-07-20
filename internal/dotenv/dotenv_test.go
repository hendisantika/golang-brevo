package dotenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  map[string]string
	}{
		{
			name:  "simple pair",
			input: "BREVO_API_KEY=xkeysib-abc123",
			want:  map[string]string{"BREVO_API_KEY": "xkeysib-abc123"},
		},
		{
			name:  "comments and blank lines are skipped",
			input: "# a comment\n\nKEY=value\n\n# trailing comment\n",
			want:  map[string]string{"KEY": "value"},
		},
		{
			name:  "export prefix is stripped",
			input: "export KEY=value",
			want:  map[string]string{"KEY": "value"},
		},
		{
			name:  "whitespace around key and value is trimmed",
			input: "  KEY  =  value  ",
			want:  map[string]string{"KEY": "value"},
		},
		{
			name:  "double quotes preserve spaces and expand escapes",
			input: `NAME="Hendi Santika"` + "\n" + `MSG="line1\nline2"`,
			want:  map[string]string{"NAME": "Hendi Santika", "MSG": "line1\nline2"},
		},
		{
			name:  "single quotes are literal",
			input: `MSG='line1\nline2'`,
			want:  map[string]string{"MSG": `line1\nline2`},
		},
		{
			name:  "inline comment stripped from bare value",
			input: "KEY=value # trailing note",
			want:  map[string]string{"KEY": "value"},
		},
		{
			name:  "hash without leading space stays in value",
			input: "KEY=pa#ssword",
			want:  map[string]string{"KEY": "pa#ssword"},
		},
		{
			name:  "hash inside quotes is not a comment",
			input: `KEY="value # not a comment"`,
			want:  map[string]string{"KEY": "value # not a comment"},
		},
		{
			name:  "empty value",
			input: "KEY=",
			want:  map[string]string{"KEY": ""},
		},
		{
			name:  "value containing equals signs",
			input: "KEY=a=b=c",
			want:  map[string]string{"KEY": "a=b=c"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(strings.NewReader(tt.input))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for k, want := range tt.want {
				if got[k] != want {
					t.Errorf("[%s] = %q, want %q", k, got[k], want)
				}
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	tests := map[string]string{
		"missing equals": "JUST_A_KEY",
		"empty key":      "=value",
		"unterminated":   `KEY="unclosed`,
	}

	for name, input := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(strings.NewReader(input)); err == nil {
				t.Fatal("want error, got nil")
			}
		})
	}
}

func TestLoadSetsEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("DOTENV_TEST_KEY=from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Setenv("DOTENV_TEST_KEY", "") // registers cleanup
	os.Unsetenv("DOTENV_TEST_KEY")

	if err := Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := os.Getenv("DOTENV_TEST_KEY"); got != "from-file" {
		t.Errorf("got %q, want %q", got, "from-file")
	}
}

func TestLoadDoesNotOverrideExistingEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("DOTENV_TEST_KEY=from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A value already exported in the shell must win over the file.
	t.Setenv("DOTENV_TEST_KEY", "from-shell")

	if err := Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := os.Getenv("DOTENV_TEST_KEY"); got != "from-shell" {
		t.Errorf("got %q, want %q — file overrode the shell", got, "from-shell")
	}
}

func TestLoadMissingFileIsNotAnError(t *testing.T) {
	if err := Load(filepath.Join(t.TempDir(), "does-not-exist")); err != nil {
		t.Fatalf("want nil for missing file, got %v", err)
	}
}

func TestLoadMalformedFileErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("NOT_A_PAIR\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Load(path); err == nil {
		t.Fatal("want error for malformed file, got nil")
	}
}
