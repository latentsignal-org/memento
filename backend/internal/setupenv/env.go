package setupenv

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"memento/backend/internal/config"
)

var backupState struct {
	sync.Mutex
	paths map[string]bool
}

func FindProjectRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	for i := 0; i < 5; i++ {
		if fileExists(filepath.Join(dir, "package.json")) && fileExists(filepath.Join(dir, "backend", "go.mod")) {
			return dir
		}
		if fileExists(filepath.Join(dir, "go.mod")) && filepath.Base(dir) == "backend" {
			return filepath.Dir(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "."
}

func EnvPath() string {
	return filepath.Join(FindProjectRoot(), ".env")
}

func ReadEnvKeys(path string) map[string]struct{} {
	out := map[string]struct{}{}
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.Index(line, "="); idx > 0 {
			out[strings.TrimSpace(line[:idx])] = struct{}{}
		}
	}
	return out
}

// BackupOnce copies an existing env file before the first write in this process.
func BackupOnce(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	backupState.Lock()
	defer backupState.Unlock()
	if backupState.paths == nil {
		backupState.paths = map[string]bool{}
	}
	if backupState.paths[abs] {
		return "", nil
	}
	data, err := os.ReadFile(abs)
	if os.IsNotExist(err) {
		backupState.paths[abs] = true
		return "", nil
	}
	if err != nil {
		return "", err
	}
	stamp := time.Now().Format("20060102-150405")
	backup := abs + ".backup-" + stamp
	if err := os.WriteFile(backup, data, 0600); err != nil {
		return "", err
	}
	backupState.paths[abs] = true
	return backup, nil
}

func WriteKey(key, value string) error {
	return WriteKeyAt(EnvPath(), key, value)
}

func WriteKeyAt(path, key, value string) error {
	if strings.ContainsAny(key, "=\r\n") || strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("environment keys and values must be single-line")
	}
	if _, err := BackupOnce(path); err != nil {
		return fmt.Errorf("back up %s: %w", path, err)
	}
	existing, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var lines []string
	updated := false
	for _, line := range strings.Split(string(existing), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, key+"=") || strings.HasPrefix(trimmed, key+" =") {
			lines = append(lines, key+"="+value)
			updated = true
		} else {
			lines = append(lines, line)
		}
	}
	if !updated {
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines = lines[:len(lines)-1]
		}
		lines = append(lines, key+"="+value)
	}
	content := strings.Join(lines, "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	return os.WriteFile(path, []byte(content), 0600)
}

// EnsureAgentEnv seeds optional internal-tool and development defaults while
// preserving existing values.
func EnsureAgentEnv() (generatedToken bool, err error) {
	path := EnvPath()
	existing := ReadEnvKeys(path)
	if _, ok := existing[config.EnvInternalToken]; !ok {
		var buf [32]byte
		if _, err := rand.Read(buf[:]); err != nil {
			return false, fmt.Errorf("generate %s: %w", config.EnvInternalToken, err)
		}
		if err := WriteKey(config.EnvInternalToken, hex.EncodeToString(buf[:])); err != nil {
			return false, err
		}
		generatedToken = true
	}
	defaults := []struct{ key, value string }{
		{config.EnvBackendURL, fmt.Sprintf("http://127.0.0.1:%d", config.DefaultBackendPort)},
		{config.EnvAgentStepLimit, "20"},
		{config.EnvAgentModel, config.DefaultModelForProvider(config.ProviderGemini)},
	}
	for _, item := range defaults {
		if _, ok := existing[item.key]; ok {
			continue
		}
		if err := WriteKey(item.key, item.value); err != nil {
			return generatedToken, err
		}
	}
	return generatedToken, nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
