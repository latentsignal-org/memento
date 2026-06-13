package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"memento/backend/internal/config"
	"memento/backend/internal/setup"
	"memento/backend/internal/store"
)

func runInit(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	dbPath := fs.String("db", "", "msgvault SQLite database path (overrides config)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	fmt.Println()
	fmt.Println("Memento Setup")
	fmt.Println(strings.Repeat("─", 50))

	// Resolve DB path
	resolvedPath := *dbPath
	if resolvedPath == "" {
		var pathErr error
		resolvedPath, pathErr = config.ResolveMsgvaultDBPath()
		if pathErr != nil {
			resolvedPath = initPrompt("Path to msgvault.db", "")
			if resolvedPath == "" {
				return fmt.Errorf("database path is required")
			}
		}
	}
	fmt.Printf("Archive: %s\n\n", resolvedPath)

	// Open DB so existing owner defaults can be read before the shared pipeline
	// performs migrations and deterministic extraction.
	db, err := store.Open(resolvedPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	// Prompt for owner identity
	fmt.Println()
	fmt.Println("Tell us about yourself (used in the UI):")
	existingName, _ := store.GetConfig(ctx, db, "owner_name")
	existingEmail, _ := store.GetConfig(ctx, db, "owner_email")
	if existingEmail == "" {
		existingEmail = setup.InferOwnerEmail(ctx, db)
	}

	name := initPrompt("  Your name", existingName)
	if name == "" {
		return fmt.Errorf("name is required")
	}
	email := initPrompt("  Your email", existingEmail)
	if email == "" {
		return fmt.Errorf("email is required")
	}

	modelProvider, modelName, modelBaseURL, apiKey, err := initPromptModelEnv()
	if err != nil {
		return err
	}

	fmt.Println()
	params := setup.InitParams{
		OwnerName: name, OwnerEmail: email, ModelProvider: modelProvider,
		Model: modelName, ModelBaseURL: modelBaseURL, ModelAPIKey: apiKey,
		MsgvaultDBPath: resolvedPath,
	}
	summary, err := setup.RunInit(ctx, db, params, func(_ string, done, total int, detail string) {
		fmt.Printf("[%d/%d] %s...\n", done, total, detail)
	})
	if err != nil {
		return err
	}

	// Build the binary so future commands don't need `go run`
	fmt.Print("Building memento binary...          ")
	builtBinary := buildBinary()
	if builtBinary {
		fmt.Println("done (./memento)")
	} else {
		fmt.Println("skipped (use `go run ./cmd/memento`)")
	}

	// Summary
	fmt.Println()
	fmt.Println(strings.Repeat("─", 50))
	fmt.Printf("Setup complete! Hi, %s.\n\n", name)
	fmt.Printf("  Persons resolved:   %d\n", summary.Persons)
	fmt.Printf("  Human contacts:     %d\n", summary.Humans)
	fmt.Printf("  Excluded (bots):    %d\n", summary.Excluded)
	fmt.Printf("  Newsletter sources: %d\n", summary.Newsletters)
	if summary.Duplicates > 0 {
		fmt.Printf("  Possible duplicates:%d\n", summary.Duplicates)
	}
	for _, warning := range summary.Warnings {
		fmt.Printf("  Warning: %s — %s\n", warning.Name, warning.Detail)
	}
	fmt.Println()
	if summary.Duplicates > 0 {
		fmt.Println("Tidy up duplicate persons (optional but recommended):")
		fmt.Println("  ./memento person-merge-suggest          # review the suggested pairs")
		fmt.Println("  ./memento person-merge --from X --into Y # apply the ones you confirm")
		fmt.Println("  ./memento refresh                        # rebuild after merging")
		fmt.Println()
	}
	fmt.Println("Next steps:")
	fmt.Println("  1. Start the API server:  go run ./cmd/memento serve   (from the current backend/ directory)")
	fmt.Println("  2. In a second terminal:  cd .. && pnpm install && pnpm run dev")
	fmt.Println("  3. Open http://localhost:3000")
	fmt.Println()
	return nil
}

// buildBinary compiles the memento binary into the current directory.
// Returns true on success. Silently skips if `go` is not on PATH or the
// source tree isn't present (e.g. running from a pre-built binary already).
func buildBinary() bool {
	cmd := exec.Command("go", "build", "-o", "memento", "./cmd/memento")
	cmd.Dir = "."
	return cmd.Run() == nil
}

var initScanner *bufio.Scanner

func getInitScanner() *bufio.Scanner {
	if initScanner == nil {
		initScanner = bufio.NewScanner(os.Stdin)
	}
	return initScanner
}

func initPrompt(label, defaultVal string) string {
	if defaultVal != "" {
		fmt.Printf("%s [%s]: ", label, defaultVal)
	} else {
		fmt.Printf("%s: ", label)
	}
	s := getInitScanner()
	s.Scan()
	val := strings.TrimSpace(s.Text())
	if val == "" {
		return defaultVal
	}
	return val
}

func initPromptOptional(label string) string {
	fmt.Printf("%s: ", label)
	s := getInitScanner()
	s.Scan()
	return strings.TrimSpace(s.Text())
}

func initPromptClearable(label, defaultVal string) string {
	if defaultVal != "" {
		fmt.Printf("%s [%s] (type '-' to clear): ", label, defaultVal)
	} else {
		fmt.Printf("%s: ", label)
	}
	s := getInitScanner()
	s.Scan()
	val := strings.TrimSpace(s.Text())
	if val == "-" {
		return ""
	}
	if val == "" {
		return defaultVal
	}
	return val
}

func initPromptSecret(label string, configured bool) string {
	if configured {
		fmt.Printf("%s [configured, Enter to keep]: ", label)
	} else {
		fmt.Printf("%s (Enter to skip): ", label)
	}
	s := getInitScanner()
	s.Scan()
	return strings.TrimSpace(s.Text())
}

func initPromptModelEnv() (provider, model, baseURL, apiKey string, err error) {
	fmt.Println()
	fmt.Println("Model configuration (for narrative generation):")
	existingProvider := config.ModelProvider()
	provider = initPrompt("  Provider (gemini/openai_compatible/fake)", existingProvider)
	modelDefault := config.EnvString(config.EnvAgentModel)
	if provider != existingProvider || modelDefault == "" {
		modelDefault = config.DefaultModelForProvider(provider)
	}
	model = initPrompt("  Model", modelDefault)
	if model == "" {
		return "", "", "", "", fmt.Errorf("model is required")
	}

	baseLabel := "  Model base URL"
	if provider == config.ProviderOpenAICompatible {
		baseLabel = "  Model base URL (required for openai_compatible)"
	}
	baseDefault := config.ModelBaseURL()
	if provider != existingProvider {
		baseDefault = ""
	}
	baseURL = initPromptClearable(baseLabel, baseDefault)
	if provider == config.ProviderOpenAICompatible && baseURL == "" {
		return "", "", "", "", fmt.Errorf("%s is required when provider is %s", config.EnvModelBaseURL, config.ProviderOpenAICompatible)
	}

	if provider != config.ProviderFake {
		apiKey = initPromptSecret("  Model API key", provider == existingProvider && config.ModelAPIKey() != "")
	}
	err = setup.ValidateInitParams(setup.InitParams{
		OwnerName: "pending", OwnerEmail: "pending@example.invalid",
		ModelProvider: provider, Model: model, ModelBaseURL: baseURL,
		MsgvaultDBPath: "pending",
	})
	return provider, model, baseURL, apiKey, err
}
