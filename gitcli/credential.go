package gitcli

import (
	"bufio"
	"bytes"
	"fmt"
	"net/url"
	"strings"
)

// CredentialHelper is git's credential helpers, reached like its config and
// its transport: through the CLI, so whatever helper the user configured
// answers (jira-sync.md, JS3).
type CredentialHelper interface {
	// CredentialFill runs `git credential fill` for the URL's protocol and
	// host and the given username, with GIT_TERMINAL_PROMPT=0, and returns
	// the password the helpers answered with. No answer is an error naming
	// `git credential approve`, the way to store one.
	CredentialFill(url, username string) (secret string, err error)
}

var _ CredentialHelper = &repo{}

// CredentialFill asks git's credential helpers for a secret.
//
// Everything goes on standard input, the helper protocol's own channel, so a
// secret is never in an argv. Terminal prompts are off: a sync runs from cron
// as often as from a terminal, and a prompt nobody answers is a hang.
func (r *repo) CredentialFill(rawURL, username string) (string, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("credential for %q: %w", rawURL, err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("credential for %q: not an absolute URL", rawURL)
	}

	var request bytes.Buffer
	for _, attr := range [][2]string{{"protocol", u.Scheme}, {"host", u.Host}, {"username", username}} {
		if strings.ContainsAny(attr[1], "\n\x00") {
			return "", fmt.Errorf("credential for %q: %s contains a newline or a NUL", rawURL, attr[0])
		}
		if attr[1] != "" {
			fmt.Fprintf(&request, "%s=%s\n", attr[0], attr[1])
		}
	}
	request.WriteString("\n")

	stdout, stderr, code, err := r.git.runInput(request.Bytes(), []string{"GIT_TERMINAL_PROMPT=0"}, "credential", "fill")
	if err != nil {
		return "", err
	}

	store := fmt.Sprintf("store one with `git credential approve` "+
		"(protocol=%s, host=%s, username=%s, password=<token>)", u.Scheme, u.Host, username)
	if code != 0 {
		msg := strings.TrimSpace(string(stderr))
		if msg == "" {
			msg = fmt.Sprintf("exit status %d", code)
		}
		return "", fmt.Errorf("no credential for %s://%s from git's credential helpers (%s); %s",
			u.Scheme, u.Host, msg, store)
	}

	secret := credentialAttribute(stdout, "password")
	if secret == "" {
		return "", fmt.Errorf("no credential for %s://%s from git's credential helpers; %s",
			u.Scheme, u.Host, store)
	}
	return secret, nil
}

// credentialAttribute reads one attribute of a helper's answer,
// `key=value` lines as gitcredentials(7) describes them.
func credentialAttribute(answer []byte, key string) string {
	scanner := bufio.NewScanner(bytes.NewReader(answer))
	for scanner.Scan() {
		name, value, ok := strings.Cut(scanner.Text(), "=")
		if ok && name == key {
			return value
		}
	}
	return ""
}
