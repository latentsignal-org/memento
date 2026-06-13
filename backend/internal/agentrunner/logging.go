package agentrunner

import (
	"log"
	"os"

	"memento/backend/internal/config"
)

var (
	colorRed    string
	colorYellow string
	colorReset  string
)

func init() {
	if os.Getenv("NO_COLOR") != "" {
		return
	}
	info, err := os.Stderr.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return
	}
	colorRed = "\x1b[31m"
	colorYellow = "\x1b[33m"
	colorReset = "\x1b[0m"
}

func agentInfof(format string, args ...any) {
	if !config.AgentVerboseLogs() {
		return
	}
	log.Printf(format, args...)
}

func agentWarnf(format string, args ...any) {
	log.Printf(colorYellow+format+colorReset, args...)
}

func agentErrorf(format string, args ...any) {
	log.Printf(colorRed+format+colorReset, args...)
}
