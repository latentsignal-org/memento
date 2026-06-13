package main

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"memento/backend/internal/config"
	"memento/backend/internal/setup"
)

// runApp starts the Memento server (identical to `memento serve`) and opens
// the bundled web UI in the default browser once the server reports healthy.
//
// This is the primary end-user entry point for binary releases: one process
// serves both the API and the statically exported frontend on 127.0.0.1.
func runApp(ctx context.Context, args []string) error {
	port := config.DefaultBackendPort
	noOpen := false
	isDemo := false
	explicitDB := ""
	passthrough := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		// value reads the value for a flag given either as "--flag value" or
		// "--flag=value", consuming the following arg only in the former case.
		value := func(prefix string) (string, bool) {
			if v, ok := strings.CutPrefix(arg, prefix+"="); ok {
				return v, true
			}
			if arg == prefix && i+1 < len(args) {
				return args[i+1], true
			}
			return "", false
		}
		switch {
		case arg == "--no-open":
			noOpen = true
			continue // not a `serve` flag; do not pass through
		case arg == "--demo":
			isDemo = true
		case strings.HasPrefix(arg, "--db"):
			if v, ok := value("--db"); ok {
				explicitDB = v
			}
		case strings.HasPrefix(arg, "--port"):
			if v, ok := value("--port"); ok {
				if n, err := strconv.Atoi(v); err == nil {
					port = n
				}
			}
		}
		passthrough = append(passthrough, arg)
	}

	// Pre-flight: validate the msgvault archive before starting the server.
	// Without this, the raw SQLite error ("unable to open database file") gives
	// a new user no clue what to do next. Demo mode seeds its own DB, so skip.
	// We reuse the same check `memento doctor` runs, so existence, file-vs-dir,
	// read-only openability, and required-table presence are all covered.
	if !isDemo {
		dbPath, err := config.ResolveMsgvaultDBPath()
		if explicitDB != "" {
			dbPath = explicitDB
		} else if err != nil {
			return fmt.Errorf(
				"could not locate a msgvault archive: %v\n\n"+
					"  → Try the demo first:   ./memento app --demo\n"+
					"  → Run a health check:   ./memento doctor\n"+
					"  → Install msgvault:     https://www.msgvault.io/\n"+
					"  → Point to your DB:     MEMENTO_MSGVAULT_DB=/path/to/msgvault.db ./memento app\n",
				err,
			)
		}
		if check := setup.CheckMsgvault(ctx, dbPath); check.Status == setup.StatusFail {
			return fmt.Errorf(
				"%s\n\n"+
					"  → %s\n"+
					"  → Try the demo first:   ./memento app --demo\n"+
					"  → Run a full check:     ./memento doctor\n",
				check.Detail, check.Hint,
			)
		}
	}

	url := fmt.Sprintf("http://127.0.0.1:%d/", port)
	if !noOpen {
		go openWhenHealthy(ctx, url)
	}
	fmt.Printf("Memento app starting — UI at %s\n", url)
	return runServe(ctx, passthrough)
}

// openWhenHealthy polls /api/health and launches the default browser once
// the server is accepting requests (or gives up quietly after ~15s).
func openWhenHealthy(ctx context.Context, url string) {
	client := &http.Client{Timeout: time.Second}
	for i := 0; i < 30; i++ {
		select {
		case <-ctx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
		resp, err := client.Get(url + "api/health")
		if err != nil {
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			if err := openBrowser(url); err != nil {
				fmt.Printf("open %s in your browser (auto-open failed: %v)\n", url, err)
			}
			return
		}
	}
	fmt.Printf("open %s in your browser\n", url)
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
