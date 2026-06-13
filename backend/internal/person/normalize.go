package person

import (
	"regexp"
	"strings"
)

// normalizeEmail lowercases and strips plus-tags (alice+newsletter@gmail.com
// -> alice@gmail.com). Plus-tag stripping only applies inside the local part.
func normalizeEmail(email string) string {
	email = strings.ToLower(strings.TrimSpace(email))
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return email
	}
	local := email[:at]
	domain := email[at:]
	if plus := strings.Index(local, "+"); plus >= 0 && shouldStripPlusTag(local[:plus], domain[1:]) {
		local = local[:plus]
	}
	return local + domain
}

// shouldStripPlusTag intentionally errs on the side of not stripping. Real
// user mailboxes like ann.jose+home@gmail.com should collapse, but generated
// relay addresses like buzz+random@gmail.com must stay distinct or the
// resolver will create giant cross-person clusters.
func shouldStripPlusTag(baseLocal, domain string) bool {
	if baseLocal == "" || isSystemLocalPart(baseLocal) {
		return false
	}
	if !supportsPlusAddressingDomain(domain) {
		return false
	}
	// Restrict stripping to mailbox-shaped locals; this avoids collapsing
	// generated aliases such as buzz+...@gmail.com that are not personal
	// subaddressing even though they are hosted on a plus-capable domain.
	return strings.ContainsAny(baseLocal, "._-") || len(baseLocal) >= 5
}

func supportsPlusAddressingDomain(domain string) bool {
	switch strings.ToLower(strings.TrimSpace(domain)) {
	case "gmail.com", "googlemail.com", "hey.com", "fastmail.com", "pm.me", "proton.me", "protonmail.com":
		return true
	default:
		return false
	}
}

// normalizeName lowercases, trims, collapses whitespace, and strips a trailing
// forwarder parenthetical so "Jane Smith (via Google Photos)" and
// "Jane Smith" share a key. Used purely for clustering — not stored.
func normalizeName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if stripped := stripForwarderParenthetical(name); stripped != "" {
		name = stripped
	}
	name = strings.ToLower(name)
	name = whitespaceRun.ReplaceAllString(name, " ")
	return strings.TrimSpace(name)
}

// stripForwarderParenthetical removes a trailing "(...)" when the contents
// look like a forwarder marker. Returns the cleaned name, or "" if nothing
// was stripped.
func stripForwarderParenthetical(name string) string {
	m := trailingParen.FindStringSubmatch(name)
	if m == nil {
		return ""
	}
	inner := strings.ToLower(strings.TrimSpace(m[2]))
	if !looksLikeForwarderMarker(inner) {
		return ""
	}
	return strings.TrimSpace(m[1])
}

func looksLikeForwarderMarker(inner string) bool {
	if strings.HasPrefix(inner, "via ") {
		return true
	}
	switch inner {
	case "google docs", "google photos", "google drive", "google calendar",
		"sms", "patreon", "substack", "medium", "linkedin", "slack",
		"github", "the gates notes", "lemon squeezy":
		return true
	}
	// "X (Y Z)" where Y is "google" usually indicates a Google forwarder.
	if strings.HasPrefix(inner, "google ") {
		return true
	}
	return false
}

// displayNameTokens splits a display name into lower-case tokens, dropping
// punctuation and one-letter initials. Used by the Jaccard pass.
func displayNameTokens(name string) []string {
	name = normalizeName(name)
	if name == "" {
		return nil
	}
	raw := tokenSplit.Split(name, -1)
	var out []string
	for _, t := range raw {
		t = strings.Trim(t, ".'`-")
		if len(t) < 2 {
			continue
		}
		out = append(out, t)
	}
	return out
}

var (
	whitespaceRun = regexp.MustCompile(`\s+`)
	trailingParen = regexp.MustCompile(`^(.*?)\s*\(([^()]+)\)\s*$`)
	tokenSplit    = regexp.MustCompile(`[\s,;:/]+`)
)
