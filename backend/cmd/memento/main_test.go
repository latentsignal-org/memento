package main

import (
	"errors"
	"strings"
	"testing"
)

func TestFormatCLIErrorPlain(t *testing.T) {
	got := formatCLIError(errors.New("boom"))
	if !strings.Contains(got, "ERROR: boom") {
		t.Fatalf("formatCLIError() = %q, want plain error text", got)
	}
}

func TestRefreshSelection(t *testing.T) {
	tests := []struct {
		name string
		in   refreshSelection
		want refreshSelection
	}{
		{
			name: "plain refresh runs dimensions and social only",
			in:   refreshSelection{},
			want: refreshSelection{People: true, Newsletters: true, Projects: true, Concepts: true, Social: true},
		},
		{
			name: "avatars only",
			in:   refreshSelection{Avatars: true},
			want: refreshSelection{Avatars: true},
		},
		{
			name: "people and avatars",
			in:   refreshSelection{People: true, Avatars: true},
			want: refreshSelection{People: true, Social: true, Avatars: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.in.resolve()
			if got != tt.want {
				t.Fatalf("resolve() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
