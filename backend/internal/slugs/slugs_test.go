package slugs

import "testing"

func TestValidateEntitySlug(t *testing.T) {
	valid := []string{"jane-doe", "project-2026", "ask", "a"}
	for _, slug := range valid {
		if err := ValidateEntitySlug(slug); err != nil {
			t.Fatalf("ValidateEntitySlug(%q) returned error: %v", slug, err)
		}
	}

	invalid := []string{
		"",
		" jane",
		"jane ",
		"Jane",
		"jane_doe",
		"jane--doe",
		"-jane",
		"jane-",
		"jane/doe",
		"_",
		"new",
		"merge-review",
	}
	for _, slug := range invalid {
		if err := ValidateEntitySlug(slug); err == nil {
			t.Fatalf("ValidateEntitySlug(%q) succeeded, want error", slug)
		}
	}
}
