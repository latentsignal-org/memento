package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunDoctorJSONReportsFailures(t *testing.T) {
	var output bytes.Buffer
	oldOutput := doctorOutput
	doctorOutput = &output
	t.Cleanup(func() { doctorOutput = oldOutput })

	err := runDoctor(context.Background(), []string{"--json", "--db", filepath.Join(t.TempDir(), "missing.sqlite")})
	if !errors.Is(err, errDoctorFailed) {
		t.Fatalf("runDoctor error = %v, want errDoctorFailed", err)
	}
	var report doctorReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatalf("decode output: %v\n%s", err, output.String())
	}
	if report.Summary.Fail == 0 || report.Summary.Healthy {
		t.Fatalf("summary = %+v, want failures", report.Summary)
	}
	if len(report.Checks) == 0 {
		t.Fatal("JSON report has no checks")
	}
	if report.DatabasePath == "" {
		t.Fatal("JSON report did not include database path")
	}
}

func TestRunDoctorTextPrintsDatabasePath(t *testing.T) {
	var output bytes.Buffer
	oldOutput := doctorOutput
	doctorOutput = &output
	t.Cleanup(func() { doctorOutput = oldOutput })

	path := filepath.Join(t.TempDir(), "missing.sqlite")
	err := runDoctor(context.Background(), []string{"--db", path})
	if !errors.Is(err, errDoctorFailed) {
		t.Fatalf("runDoctor error = %v, want errDoctorFailed", err)
	}
	if !strings.Contains(output.String(), "Database — "+path) {
		t.Fatalf("doctor output missing database path:\n%s", output.String())
	}
	if !strings.Contains(output.String(), "Project root — ") {
		t.Fatalf("doctor output missing project root:\n%s", output.String())
	}
	for _, section := range []string{"Archive\n", "Tools\n", "App\n"} {
		if !strings.Contains(output.String(), section) {
			t.Fatalf("doctor output missing %q section:\n%s", strings.TrimSpace(section), output.String())
		}
	}
	if strings.Index(output.String(), "Archive\n") > strings.Index(output.String(), "Database — "+path) {
		t.Fatalf("database row is not under archive section:\n%s", output.String())
	}
	if strings.Index(output.String(), "Tools\n") > strings.Index(output.String(), "Project root — ") {
		t.Fatalf("project root row is not under tools section:\n%s", output.String())
	}
	if !strings.Contains(output.String(), "✕") {
		t.Fatalf("doctor output missing status icon:\n%s", output.String())
	}
}

func TestRunDoctorTextOmitsProjectRootOutsideSourceCheckout(t *testing.T) {
	var output bytes.Buffer
	oldOutput := doctorOutput
	doctorOutput = &output
	t.Cleanup(func() { doctorOutput = oldOutput })
	withWorkingDirectory(t, t.TempDir())

	path := filepath.Join(t.TempDir(), "missing.sqlite")
	err := runDoctor(context.Background(), []string{"--db", path})
	if !errors.Is(err, errDoctorFailed) {
		t.Fatalf("runDoctor error = %v, want errDoctorFailed", err)
	}
	if strings.Contains(output.String(), "Project root — ") {
		t.Fatalf("doctor output should not print project root outside a checkout:\n%s", output.String())
	}
}

func withWorkingDirectory(t *testing.T, path string) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(path); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(old) })
}
