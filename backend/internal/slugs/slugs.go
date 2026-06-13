package slugs

import (
	"fmt"
	"regexp"
	"strings"
)

var entitySlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

var reservedPathSegments = map[string]bool{
	"_":            true,
	"new":          true,
	"merge-review": true,
}

func ReservedPathSegment(slug string) bool {
	return reservedPathSegments[strings.TrimSpace(slug)]
}

func ValidateEntitySlug(slug string) error {
	if slug != strings.TrimSpace(slug) {
		return fmt.Errorf("invalid slug %q: must not contain leading or trailing whitespace", slug)
	}
	if slug == "" {
		return fmt.Errorf("invalid slug: must not be empty")
	}
	if ReservedPathSegment(slug) {
		return fmt.Errorf("invalid slug %q: reserved for application routes", slug)
	}
	if !entitySlugPattern.MatchString(slug) {
		return fmt.Errorf("invalid slug %q: use lowercase letters, numbers, and single hyphens", slug)
	}
	return nil
}
