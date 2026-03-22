package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var osUserHomeDirFunc = os.UserHomeDir

func ResolveBaseOutputDir(outputDir, defaultDir string) (string, error) {
	trimmed := strings.TrimSpace(outputDir)
	if trimmed == "" {
		return filepath.Clean(strings.TrimSpace(defaultDir)), nil
	}

	if strings.HasPrefix(trimmed, "~/") {
		homeDir, err := osUserHomeDirFunc()
		if err != nil {
			return "", fmt.Errorf("failed to resolve home directory for output_dir: %w", err)
		}
		trimmed = filepath.Join(homeDir, strings.TrimPrefix(trimmed, "~/"))
	}

	return filepath.Clean(trimmed), nil
}
