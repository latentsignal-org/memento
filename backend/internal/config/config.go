package config

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolveMsgvaultDBPath finds the local msgvault SQLite database path using
// Memento's explicit override first, then msgvault's home/config conventions.
func ResolveMsgvaultDBPath() (string, error) {
	if explicit := os.Getenv(EnvMsgvaultDB); explicit != "" {
		return explicit, nil
	}

	if home := os.Getenv(EnvMsgvaultHome); home != "" {
		return filepath.Join(home, "msgvault.db"), nil
	}

	configPath := filepath.Join(userHomeDir(), ".msgvault", "config.toml")
	dataDir, err := readDataDir(configPath)
	if err == nil && dataDir != "" {
		return filepath.Join(dataDir, "msgvault.db"), nil
	}

	return filepath.Join(userHomeDir(), ".msgvault", "msgvault.db"), nil
}

func readDataDir(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	section := ""
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.Trim(line, "[]")
			continue
		}
		if section != "data" || !strings.HasPrefix(line, "data_dir") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return "", fmt.Errorf("invalid data_dir line in %s", path)
		}
		return strings.Trim(strings.TrimSpace(parts[1]), `"`), nil
	}
	return "", scanner.Err()
}

func userHomeDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return home
}

// LoadDotEnv searches for a .env or .dev.vars file in the current working directory
// or its parents (up to 5 levels) and loads it into the environment. Values from
// the file intentionally override an inherited process environment so restarting
// local Memento picks up edits to .env deterministically.
func LoadDotEnv() {
	if os.Getenv("MEMENTO_SKIP_DOTENV") == "1" {
		return
	}

	dir, err := os.Getwd()
	if err != nil {
		return
	}

	var envPath string
	for i := 0; i < 5; i++ {
		p := filepath.Join(dir, ".env")
		if _, err := os.Stat(p); err == nil {
			envPath = p
			break
		}
		p = filepath.Join(dir, ".dev.vars")
		if _, err := os.Stat(p); err == nil {
			envPath = p
			break
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	if envPath == "" {
		return
	}

	file, err := os.Open(envPath)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		// Strip quotes if present
		if (strings.HasPrefix(val, `"`) && strings.HasSuffix(val, `"`)) ||
			(strings.HasPrefix(val, `'`) && strings.HasSuffix(val, `'`)) {
			val = val[1 : len(val)-1]
		}

		os.Setenv(key, val)
	}
}
