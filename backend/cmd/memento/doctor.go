package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"memento/backend/internal/config"
	"memento/backend/internal/setup"
	"memento/backend/internal/setupenv"
)

var errDoctorFailed = errors.New("doctor found hard failures")
var doctorOutput io.Writer = os.Stdout

type doctorSummary struct {
	OK      int  `json:"ok"`
	Warn    int  `json:"warn"`
	Fail    int  `json:"fail"`
	Healthy bool `json:"healthy"`
}

type doctorReport struct {
	DatabasePath  string        `json:"databasePath"`
	DatabaseError string        `json:"databaseError,omitempty"`
	ProjectRoot   string        `json:"projectRoot"`
	Checks        []setup.Check `json:"checks"`
	Summary       doctorSummary `json:"summary"`
}

func runDoctor(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	dbPath := fs.String("db", "", "msgvault SQLite database path")
	jsonOut := fs.Bool("json", false, "emit structured JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}

	resolvedDB, resolveErr := doctorDatabasePath(*dbPath)
	checks := setup.RunAllChecks(ctx, *dbPath)
	report := doctorReport{DatabasePath: resolvedDB, ProjectRoot: doctorProjectRoot(), Checks: checks}
	if resolveErr != nil {
		report.DatabaseError = resolveErr.Error()
	}
	for _, check := range checks {
		switch check.Status {
		case setup.StatusOK:
			report.Summary.OK++
		case setup.StatusWarn:
			report.Summary.Warn++
		case setup.StatusFail:
			report.Summary.Fail++
		}
	}
	report.Summary.Healthy = report.Summary.Fail == 0

	if *jsonOut {
		encoder := json.NewEncoder(doctorOutput)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			return err
		}
	} else {
		fmt.Fprintln(doctorOutput, "Memento Doctor")
		fmt.Fprintln(doctorOutput)
		useColor := doctorUseColor()
		displayChecks := make([]setup.Check, 0, len(checks)+1)
		databaseCheck := setup.Check{Name: "Database", Status: setup.StatusOK, Detail: report.DatabasePath}
		if report.DatabaseError != "" {
			databaseCheck.Status = setup.StatusFail
			databaseCheck.Detail = report.DatabasePath + " (" + report.DatabaseError + ")"
			databaseCheck.Hint = "Run `msgvault stats` or pass `--db PATH`."
		}
		displayChecks = append(displayChecks, databaseCheck)

		var projectRootPrinted bool
		for _, check := range checks {
			if check.Name == "Project root" && report.ProjectRoot != "" {
				displayChecks = append(displayChecks, check)
				projectRootPrinted = true
				break
			}
		}
		if !projectRootPrinted && report.ProjectRoot != "" {
			displayChecks = append(displayChecks, setup.Check{Name: "Project root", Status: setup.StatusOK, Detail: report.ProjectRoot})
		}

		for _, check := range checks {
			if check.Name == "Project root" {
				continue
			}
			displayChecks = append(displayChecks, check)
		}
		printDoctorChecks(displayChecks, useColor)
		fmt.Fprintln(doctorOutput)
		fmt.Fprintf(doctorOutput, "Summary: %d ok, %d warning(s), %d failure(s)\n", report.Summary.OK, report.Summary.Warn, report.Summary.Fail)
	}

	if report.Summary.Fail > 0 {
		return errDoctorFailed
	}
	return nil
}

func printDoctorChecks(checks []setup.Check, useColor bool) {
	groups := []struct {
		title string
		names map[string]bool
	}{
		{title: "Archive", names: map[string]bool{"Database": true, "Msgvault archive": true, "Keyword search": true, "Vector search": true}},
		{title: "Tools", names: map[string]bool{"Project root": true, "Go": true, "Node.js": true, "pnpm": true, "msgvault": true}},
		{title: "Environment", names: map[string]bool{"Node dependencies": true, "Environment file": true, "Runtime defaults": true}},
		{title: "App", names: map[string]bool{"Owner": true, "Onboarding": true, "App data": true, "Memory indexes": true, "AI Provider": true}},
	}

	printed := make([]bool, len(checks))
	var wroteGroup bool
	for _, group := range groups {
		groupStarted := false
		for i, check := range checks {
			if printed[i] || !group.names[check.Name] {
				continue
			}
			if !groupStarted {
				printDoctorGroupTitle(group.title, wroteGroup)
				groupStarted = true
				wroteGroup = true
			}
			printDoctorCheck(check, useColor)
			printed[i] = true
		}
	}

	var otherStarted bool
	for i, check := range checks {
		if printed[i] {
			continue
		}
		if !otherStarted {
			printDoctorGroupTitle("Other", wroteGroup)
			otherStarted = true
			wroteGroup = true
		}
		printDoctorCheck(check, useColor)
	}
}

func printDoctorGroupTitle(title string, leadingBlank bool) {
	if leadingBlank {
		fmt.Fprintln(doctorOutput)
	}
	fmt.Fprintln(doctorOutput, title)
}

func printDoctorCheck(check setup.Check, useColor bool) {
	fmt.Fprintf(doctorOutput, "%s  %s — %s\n", doctorStatusLabel(check.Status, useColor), check.Name, check.Detail)
	if check.Status != setup.StatusOK && check.Hint != "" {
		fmt.Fprintf(doctorOutput, "   → %s\n", check.Hint)
	}
}

func doctorDatabasePath(dbPath string) (string, error) {
	if dbPath != "" {
		abs, err := filepath.Abs(dbPath)
		if err != nil {
			return dbPath, err
		}
		return abs, nil
	}
	path, err := config.ResolveMsgvaultDBPath()
	if err != nil {
		return "(unresolved)", err
	}
	return path, nil
}

func doctorProjectRoot() string {
	root := setupenv.FindProjectRoot()
	if root == "." {
		abs, err := filepath.Abs(root)
		if err == nil && fileExists(filepath.Join(abs, "package.json")) && fileExists(filepath.Join(abs, "backend", "go.mod")) {
			return abs
		}
		return ""
	}
	return root
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func doctorUseColor() bool {
	file, ok := doctorOutput.(*os.File)
	return ok && supportsANSI(file)
}

func doctorStatusLabel(status setup.Status, color bool) string {
	icon := "✓"
	if status == setup.StatusWarn {
		icon = "!"
	} else if status == setup.StatusFail {
		icon = "✕"
	}
	if !color {
		return icon
	}
	switch status {
	case setup.StatusOK:
		return "\033[32;1m" + icon + "\033[0m"
	case setup.StatusWarn:
		return "\033[33;1m" + icon + "\033[0m"
	case setup.StatusFail:
		return "\033[31;1m" + icon + "\033[0m"
	default:
		return icon
	}
}
