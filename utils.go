package main

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func FormatSizeGB(size int64) string {
	gb := float64(size) / (1024 * 1024 * 1024)
	return fmt.Sprintf("%.1f GB", gb)
}

func extractCodeBlocks(input string) []string {
	var codeBlocks []string
	var currentBlock strings.Builder
	inCodeBlock := false
	isGoBlock := false

	scanner := bufio.NewScanner(strings.NewReader(input))
	for scanner.Scan() {
		line := scanner.Text()

		if strings.HasPrefix(line, "```") {
			if !inCodeBlock {
				inCodeBlock = true
				isGoBlock = strings.HasPrefix(line, "```go")
				currentBlock.Reset()
			} else {
				if isGoBlock {
					codeBlocks = append(codeBlocks, currentBlock.String())
				}
				inCodeBlock = false
				isGoBlock = false
			}
		} else if inCodeBlock && isGoBlock {
			currentBlock.WriteString(line + "\n")
		}
	}

	return codeBlocks
}

// WIP: image inputs for mm models
func (m *model) loadImageAsBase64(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read image: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(path))
	var mimeType string
	switch ext {
	case ".jpg", ".jpeg":
		mimeType = "image/jpeg"
	case ".png":
		mimeType = "image/png"
	case ".gif":
		mimeType = "image/gif"
	case ".webp":
		mimeType = "image/webp"
	default:
		return "", fmt.Errorf("unsupported image format: %s", ext)
	}

	base64Data := base64.StdEncoding.EncodeToString(data)
	return fmt.Sprintf("data:%s;base64,%s", mimeType, base64Data), nil
}

func keyIsCtrlZ(msg tea.KeyMsg) bool {
	return msg.Type == tea.KeyCtrlZ
}
