package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	// Imports
)

func (m *model) toggleOllamaServe() tea.Cmd {
	return func() tea.Msg {
		if m.ollamaRunning {
			err := exec.Command("pkill", "-f", "ollama serve").Run()
			if err != nil {
				return errMsg(fmt.Errorf("failed to stop Ollama: %w", err))
			}
		} else {
			err := exec.Command("ollama", "serve").Start()
			if err != nil {
				return errMsg(fmt.Errorf("failed to start Ollama: %w", err))
			}
		}
		return OllamaToggledMsg{}
	}
}

func processAgentChain(input string, m *model, agent Agent) (string, error) {
	var contextContent string
	var err error

	if agent.UseContext && agent.ContextFilePath != "" && agent.ContextFilePath != "No context file selected" {
		contextContent, err = loadFileContext(agent.ContextFilePath)
		if err != nil {
			return "", fmt.Errorf("failed to load context for agent '%s': %w", agent.Role, err)
		}
	}

	var systemPrompt string

	hasCodeChecker := false
	for _, tool := range agent.Tools {
		if tool.Name == "check_go_code" {
			hasCodeChecker = true
			break
		}
	}

	codeBlocks := extractCodeBlocks(input)
	hasCode := len(codeBlocks) > 0

	// if an agent is given golinter tool and go code is detected, system prompt is overridden
	if hasCodeChecker && hasCode {
		systemPrompt = `You are a code review assistant. Your primary task is to analyze and test Go code.
Follow these steps for each code review:

1. Use the check_go_code tool to analyze it
    - you will ALWAYS use this tool on go code
    - print any errors or warnings you get
2. Analyze the tool's output thoroughly:
   - Build errors indicate the code won't compile
   - Linter warnings suggest potential issues
   - Pay special attention to type errors and undefined variables
3. Always provide:
   - A clear summary of all issues found
   - Specific suggestions for fixing each problem
   - Example corrections where appropriate
4. Even if the code passes checks, consider:
   - Code organization
   - Error handling
   - Best practices
   - Performance implications

Important: Always use the check_go_code tool on any Go code you receive. Do not skip this step. Do not alter any code you recieve`

		if contextContent != "" {
			systemPrompt = fmt.Sprintf("%s\n\nContext: %s", systemPrompt, contextContent)
		}
	} else {
		if agent.SystemPrompt == "" {
			systemPrompt = defaultSystemPrompt
		} else {
			systemPrompt = agent.SystemPrompt
		}

		if contextContent != "" {
			if strings.Contains(systemPrompt, "{context}") {
				systemPrompt = strings.ReplaceAll(systemPrompt, "{context}", contextContent)
			} else {
				systemPrompt = fmt.Sprintf("%s\n\nContext:\n%s", systemPrompt, contextContent)
			}
		} else {
			systemPrompt = strings.ReplaceAll(systemPrompt, "{context}", "")
			systemPrompt = strings.TrimSpace(systemPrompt)
		}
	}

	var messages []map[string]string
	messages = append(messages, map[string]string{
		"role":    "system",
		"content": systemPrompt,
	})

	if agent.UseConversation {
		messages = append(messages, m.conversationHistory...)
	}

	messages = append(messages, map[string]string{
		"role":    "user",
		"content": input,
	})

	contextWindow, err := strconv.Atoi(agent.Tokens)
	if err != nil || contextWindow <= 0 {
		contextWindow = 2048
	}

	payload := map[string]interface{}{
		"model":    agent.ModelVersion,
		"messages": messages,
		"stream":   false,
		"options": map[string]interface{}{
			"num_ctx": contextWindow,
		},
	}

	// WIP: mm support
	if strings.Contains(agent.ModelVersion, "llava") || strings.Contains(agent.ModelVersion, "bakllava") {
		for _, msg := range messages {
			if strings.Contains(msg["content"], "![") && strings.Contains(msg["content"], "](data:image") {
				payload["model"] = agent.ModelVersion
				break
			}
		}
	}

	if hasCodeChecker && hasCode {
		payload["tools"] = []map[string]interface{}{
			{
				"type": "function",
				"function": map[string]interface{}{
					"name":        "check_go_code",
					"description": "Check Go code for errors and style issues using golint.",
					"parameters": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"code": map[string]interface{}{
								"type":        "string",
								"description": "The Go code to check for errors.",
							},
						},
						"required": []string{"code"},
					},
				},
			},
		}
	}

	requestBody, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	resp, err := http.Post(ollamaAPIURL+"/chat", "application/json", bytes.NewBuffer(requestBody))
	if err != nil {
		return "", fmt.Errorf("failed to send request to Ollama API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Ollama API error: %s", string(body))
	}

	var apiResponse struct {
		Message struct {
			Role      string `json:"role"`
			Content   string `json:"content"`
			ToolCalls []struct {
				Function struct {
					Name      string          `json:"name"`
					Arguments json.RawMessage `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"message"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&apiResponse); err != nil {
		return "", fmt.Errorf("failed to decode Ollama API response: %w", err)
	}

	var fullResponse strings.Builder
	fullResponse.WriteString(fmt.Sprintf("Response from %s:\n\n", agent.Role))

	if hasCodeChecker && hasCode {
		if !strings.Contains(apiResponse.Message.Content, `{"name": "check_go_code"`) {
			fullResponse.WriteString("Initial Analysis:\n")
		}
	}
	fullResponse.WriteString(apiResponse.Message.Content)

	if len(apiResponse.Message.ToolCalls) > 0 {
		for _, toolCall := range apiResponse.Message.ToolCalls {
			if toolCall.Function.Name == "check_go_code" {
				toolCallJSON := map[string]interface{}{
					"name":       toolCall.Function.Name,
					"parameters": json.RawMessage(toolCall.Function.Arguments),
				}

				toolCallData, err := json.Marshal(toolCallJSON)
				if err != nil {
					return "", fmt.Errorf("failed to marshal tool call: %w", err)
				}

				code, err := parseToolCall(toolCallData)
				if err != nil {
					return "", fmt.Errorf("failed to parse tool call: %w", err)
				}

				lintResult, err := executeGolangciLint(code, agent.Role, m)
				if err != nil {
					analysisMessages := append(messages,
						map[string]string{
							"role":    "assistant",
							"content": apiResponse.Message.Content,
						},
						map[string]string{
							"role":    "user",
							"content": fmt.Sprintf("The code checking tool found some issues:\n\n%s\n\nPlease analyze these results and provide specific recommendations.", lintResult),
						},
					)

					analysisPayload := map[string]interface{}{
						"model":    agent.ModelVersion,
						"messages": analysisMessages,
						"stream":   false,
					}

					analysisBody, err := json.Marshal(analysisPayload)
					if err != nil {
						return "", fmt.Errorf("failed to marshal analysis request: %w", err)
					}

					analysisResp, err := http.Post(ollamaAPIURL+"/chat", "application/json", bytes.NewBuffer(analysisBody))
					if err != nil {
						return "", fmt.Errorf("failed to get lint analysis: %w", err)
					}
					defer analysisResp.Body.Close()

					var analysisResponse struct {
						Message struct {
							Content string `json:"content"`
						} `json:"message"`
					}

					if err := json.NewDecoder(analysisResp.Body).Decode(&analysisResponse); err != nil {
						return "", fmt.Errorf("failed to decode analysis response: %w", err)
					}

					fullResponse.WriteString("\n\nLint Results and Analysis:\n")
					fullResponse.WriteString(lintResult)
					fullResponse.WriteString("\n\nRecommendations:\n")
					fullResponse.WriteString(analysisResponse.Message.Content)
				} else {
					fullResponse.WriteString("\n\nCode Check Results:\n")
					fullResponse.WriteString(lintResult)
				}
			}
		}
	}

	return fullResponse.String(), nil
}

func fetchModels() ([]OllamaModel, error) {
	apiURL := ollamaAPIURL + "/tags"

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("error: %v", resp.Status)
	}

	var response struct {
		Models []OllamaModel `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, err
	}

	return response.Models, nil
}

func deleteModel(modelName string) error {
	apiURL := ollamaAPIURL + "/delete"

	requestBody, err := json.Marshal(map[string]string{
		"name": modelName,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequest("DELETE", apiURL, bytes.NewBuffer(requestBody))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("error deleting model: %v", resp.Status)
	}
	return nil
}

func requestOllama(messages []map[string]string, agent Agent) (string, error) {
	apiURL := ollamaAPIURL + "/chat"

	numCtx, err := strconv.Atoi(agent.Tokens)
	if err != nil || numCtx <= 0 {
		numCtx = 16384
	}

	options := map[string]interface{}{
		"num_ctx": numCtx,
	}

	requestBody, err := json.Marshal(map[string]interface{}{
		"model":    agent.ModelVersion,
		"messages": messages,
		"stream":   false,
		"options":  options,
	})
	if err != nil {
		return "", fmt.Errorf("failed to marshal request body: %w", err)
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(requestBody))
	if err != nil {
		return "", fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("error: %v", resp.Status)
	}

	var rawResponse map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&rawResponse); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}

	if message, ok := rawResponse["message"].(map[string]interface{}); ok {
		if content, ok := message["content"].(string); ok {
			return content, nil
		}
	}

	return "", fmt.Errorf("unexpected response format or empty response: %+v", rawResponse)
}
