# golang-brevo

Send tagged transactional emails through the [Brevo](https://www.brevo.com) API v3.
Standard library only — no external dependencies.

## Setup

Grab an API key from **Brevo → SMTP & API → API Keys**, then export it:

```bash
export BREVO_API_KEY=xkeysib-...
```

The key is only ever read from the environment, so it stays out of your shell
history and out of the repository.

## Usage

```bash
go run . \
  -from no-reply@example.com -from-name "Example" \
  -to user@example.com \
  -subject "Welcome aboard" \
  -text "Thanks for signing up." \
  -tag onboarding -tag welcome
```

```
sent to user@example.com
message id: <202607201830.12345678@smtp-relay.mailin.fr>
tags: onboarding, welcome
```

Run `go run . -h` for the full flag list.

`-to` and `-tag` are repeatable and also accept comma-separated values, so
`-tag onboarding -tag welcome` and `-tag onboarding,welcome` are equivalent.

## Tags

Tags label a message so you can filter it in the Brevo dashboard and match it
in webhook events. They are metadata — the recipient never sees them.

Tags are normalized before every send: surrounding whitespace is trimmed, empty
entries are dropped, and duplicates are removed while preserving the order you
supplied. If nothing survives normalization the field is omitted from the
request rather than sent as an empty array.

## Library use

```go
client, err := brevo.New(os.Getenv("BREVO_API_KEY"))
if err != nil {
    return err
}

email := brevo.Email{
    Sender:      brevo.Contact{Email: "no-reply@example.com", Name: "Example"},
    To:          []brevo.Contact{{Email: "user@example.com"}},
    Subject:     "Welcome aboard",
    HTMLContent: "<p>Thanks for signing up.</p>",
}

result, err := client.SendEmail(ctx, email.WithTags("onboarding", "welcome"))
```

`WithTags` returns a copy, so a base email can be reused across recipients:

```go
base := brevo.Email{Sender: sender, Subject: "Welcome", TextContent: body}

for _, user := range users {
    email := base.WithTags("onboarding", user.Plan)
    email.To = []brevo.Contact{{Email: user.Email}}
    if _, err := client.SendEmail(ctx, email); err != nil {
        log.Printf("send to %s: %v", user.Email, err)
    }
}
```

### Templates

Set `TemplateID` (and optionally `Params`) instead of `Subject`/`HTMLContent`;
validation adjusts accordingly.

```go
email := brevo.Email{
    Sender:     sender,
    To:         []brevo.Contact{{Email: "user@example.com"}},
    TemplateID: 42,
    Params:     map[string]any{"name": "Hendi"},
}
```

### Errors

Non-2xx responses come back as `*brevo.APIError` carrying Brevo's status, code,
and message:

```go
var apiErr *brevo.APIError
if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnauthorized {
    // bad or revoked key
}
```

Invalid input is caught locally by `Email.Validate()` before any request is
made, and all problems are reported together.

## Tests

```bash
go test ./...
```

Tests run against an `httptest` stub via `brevo.WithBaseURL`, so they never
contact the live API or need a key.
