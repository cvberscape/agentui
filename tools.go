package main

import (
	"encoding/json"
	"fmt"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var checkGoCodeTool = Tool{
	Name:        "check_go_code",
	Description: "Check Go code for errors and style issues using golint.",
	Parameters: map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"code": map[string]interface{}{
				"type":        "string",
				"description": "The Go code to check for errors.",
			},
		},
		"required": []string{"code"},
	},
}

func loadToolUsages(m *model) error {
	if _, err := os.Stat(m.toolUsageFilePath); os.IsNotExist(err) {
		m.toolUsages = []ToolUsage{}
		return nil
	}

	data, err := os.ReadFile(m.toolUsageFilePath)
	if err != nil {
		return fmt.Errorf("failed to read tool usages file: %w", err)
	}

	var loadedUsages []ToolUsage
	err = json.Unmarshal(data, &loadedUsages)
	if err != nil {
		return fmt.Errorf("failed to unmarshal tool usages: %w", err)
	}

	m.toolUsages = loadedUsages
	return nil
}

func parseToolCall(jsonData []byte) (string, error) {
	var toolCall struct {
		Name       string `json:"name"`
		Parameters struct {
			Code string `json:"code"`
		} `json:"parameters"`
	}

	if err := json.Unmarshal(jsonData, &toolCall); err != nil {
		return "", fmt.Errorf("failed to unmarshal tool call: %w", err)
	}

	if toolCall.Parameters.Code == "" {
		return "", fmt.Errorf("code parameter not found in tool call")
	}

	code := toolCall.Parameters.Code
	code = strings.ReplaceAll(code, "\\n", "\n")
	code = strings.ReplaceAll(code, "\\\"", "\"")
	code = strings.Trim(code, "\"\"\"")

	return code, nil
}

func executeGolangciLint(code string, agentRole string, m *model) (string, error) {
	if !strings.Contains(code, "package ") {
		code = "package main\n\n" + code
	}

	formattedBytes, err := format.Source([]byte(code))
	if err != nil {
		return fmt.Sprintf("Code Formatting Error:\n%v\n\nOriginal code:\n```go\n%s\n```",
			err, code), fmt.Errorf("code formatting failed: %w", err)
	}

	formattedCode := string(formattedBytes)

	// create temp dir to run checks
	tmpDir, err := os.MkdirTemp("", "golint_*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp directory: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	modInit := exec.Command("go", "mod", "init", "lintcheck")
	modInit.Dir = tmpDir
	if modInitOutput, err := modInit.CombinedOutput(); err != nil {
		return "", fmt.Errorf("failed to initialize Go module: %v\nOutput: %s", err, string(modInitOutput))
	}

	codeFile := filepath.Join(tmpDir, "main.go")
	if err := os.WriteFile(codeFile, []byte(formattedCode), 0644); err != nil {
		return "", fmt.Errorf("failed to write code file: %w", err)
	}

	// run golangci-lint
	cmd := exec.Command("golangci-lint", "run",
		"--disable-all",
		"--enable=govet",
		"--enable=staticcheck",
		"--enable=errcheck",
		"--enable=gosimple",
		"--enable=ineffassign",
		"--enable=typecheck",
		"--max-issues-per-linter=0",
		"--max-same-issues=0")
	cmd.Dir = tmpDir
	lintOutput, err := cmd.CombinedOutput()

	// run go build to catch compilation errors
	buildCmd := exec.Command("go", "build", "./...")
	buildCmd.Dir = tmpDir
	buildOutput, buildErr := buildCmd.CombinedOutput()

	var resultBuilder strings.Builder
	resultBuilder.WriteString("Code Analysis Results:\n\n")

	resultBuilder.WriteString("Formatted Code:\n```go\n")
	resultBuilder.WriteString(formattedCode)
	resultBuilder.WriteString("\n```\n\n")

	if buildErr != nil {
		resultBuilder.WriteString("Build Errors:\n```\n")
		resultBuilder.WriteString(string(buildOutput))
		resultBuilder.WriteString("\n```\n\n")
	} else {
		resultBuilder.WriteString("Build Status: Success ✓\n\n")
	}

	resultBuilder.WriteString("Linter Results:\n")
	if err != nil && len(lintOutput) > 0 {
		resultBuilder.WriteString("```\n")
		resultBuilder.WriteString(string(lintOutput))
		resultBuilder.WriteString("\n```\n")
	} else {
		resultBuilder.WriteString("No linting issues found ✓\n")
	}

	return resultBuilder.String(), nil
}
