package avatar

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Status string

const (
	StatusFound    Status = "found"
	StatusNotFound Status = "notfound"
)

type Row struct {
	EmailHash    string
	Status       Status
	Image        []byte
	MimeType     string
	ByteSize     int64
	UpstreamETag string
}

type FetchResult struct {
	Status       Status
	Image        []byte
	MimeType     string
	ByteSize     int64
	UpstreamETag string
}

type KnownAvatar struct {
	EmailHash string
}

func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func HashEmail(email string) string {
	sum := sha256.Sum256([]byte(NormalizeEmail(email)))
	return hex.EncodeToString(sum[:])
}

func LocalURL(email string, size int, initials string) string {
	email = NormalizeEmail(email)
	if email == "" || !strings.Contains(email, "@") {
		return ""
	}
	size = ClampSize(size)
	return fmt.Sprintf("/api/avatar/%s?s=%d&i=%s", HashEmail(email), size, url.QueryEscape(SanitizeInitials(initials)))
}

func ClampSize(size int) int {
	if size < 24 {
		return 24
	}
	if size > 512 {
		return 512
	}
	return size
}

func SanitizeInitials(initials string) string {
	initials = strings.TrimSpace(initials)
	if initials == "" {
		return "?"
	}
	var out []rune
	for _, r := range initials {
		if unicode.IsSpace(r) {
			continue
		}
		if unicode.IsControl(r) || r == utf8.RuneError {
			continue
		}
		out = append(out, unicode.ToUpper(r))
		if len(out) == 2 {
			break
		}
	}
	if len(out) == 0 {
		return "?"
	}
	return string(out)
}

func InitialsFromName(name, fallbackEmail string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		local := strings.Split(NormalizeEmail(fallbackEmail), "@")[0]
		name = strings.ReplaceAll(local, ".", " ")
		name = strings.ReplaceAll(name, "_", " ")
		name = strings.ReplaceAll(name, "-", " ")
	}
	var initials []rune
	for _, part := range strings.Fields(name) {
		for _, r := range part {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				initials = append(initials, unicode.ToUpper(r))
				break
			}
		}
		if len(initials) == 2 {
			break
		}
	}
	if len(initials) == 0 {
		return "?"
	}
	return string(initials)
}

var fallbackPalette = []string{
	"#2563eb",
	"#0f766e",
	"#7c3aed",
	"#c2410c",
	"#be123c",
	"#047857",
	"#4338ca",
	"#b45309",
	"#0369a1",
	"#4d7c0f",
}

func FallbackSVG(hash string, initials string, size int) []byte {
	size = ClampSize(size)
	initials = SanitizeInitials(initials)
	color := fallbackPalette[0]
	if b, err := hex.DecodeString(hash); err == nil && len(b) > 0 {
		color = fallbackPalette[int(b[0])%len(fallbackPalette)]
	}
	fontSize := int(float64(size) * 0.42)
	if fontSize < 12 {
		fontSize = 12
	}
	radius := size / 2
	text := html.EscapeString(initials)
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" role="img"><rect width="%d" height="%d" rx="%d" fill="%s"/><text x="50%%" y="50%%" dy=".35em" text-anchor="middle" fill="#fff" font-family="Arial, Helvetica, sans-serif" font-size="%d" font-weight="700">%s</text></svg>`,
		size, size, size, size, size, size, radius, color, fontSize, text)
	return []byte(svg)
}
