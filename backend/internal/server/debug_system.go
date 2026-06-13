package server

import (
	"net/http"
	"os"

	"memento/backend/internal/config"
)

// handleDebugSystemInfo reports the process working directory, the
// archive database path the server is reading, and the configured LLM
// provider/model/base URL. The Debug UI calls this once on load so
// every run page can show which folder the binary was launched from
// and which model endpoint backed the runs visible in the sidebar.
func (s *Server) handleDebugSystemInfo(w http.ResponseWriter, r *http.Request) {
	wd, err := os.Getwd()
	if err != nil {
		wd = ""
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"working_directory": wd,
		"msgvault_db_path":  s.reader.Path(),
		"provider":          config.ModelProvider(),
		"model":             config.AgentModelFor("", config.ModelProvider()),
		"model_base_url":    config.ModelBaseURL(),
	})
}
