package main

import (
	"time"

	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

type viewMode int

const (
	ChatView viewMode = iota
	InsertView
	ModelView
	AgentView
	AgentFormView
	AvailableModelsView
	ParameterSizesView
	DownloadingView
	ConfirmDelete
	ChatListView
	NewChatFormView
	FilePickerView
)

const (
	defaultSystemPrompt     = ""
	defaultContextFilePath  = ""
	defaultTokens           = "2048"
	defaultModelVersion     = ""
	ollamaAPIURL            = "http://localhost:11434/api"
	defaultIndicatorPrompt  = "│"
	configFormTitle         = "Chat Configuration"
	agentFormTitle          = "Agent Configuration"
	confirmDeleteAgentTitle = "Confirm Agent Deletion"
	confirmDeleteModelTitle = "Confirm Model Deletion"
	agentsFilePath          = "./agents.json"
)

type model struct {
	userMessages           []string
	assistantResponses     []string
	conversationHistory    []map[string]string
	currentUserMessage     string
	err                    error
	textarea               textarea.Model
	viewport               viewport.Model
	modelTable             table.Model
	availableTable         table.Model
	parameterSizesTable    table.Model
	width                  int
	height                 int
	loading                bool
	renderer               *glamour.TermRenderer
	ollamaRunning          bool
	config                 ChatConfig
	configForm             *huh.Form
	viewMode               viewMode
	formActive             bool
	confirmDeleteModelName string
	confirmForm            *huh.Form
	confirmResult          bool
	confirmDeleteType      string
	availableModels        []AvailableModel
	selectedAvailableModel AvailableModel
	spinner                spinner.Model
	agentsTable            table.Model
	agents                 []Agent
	selectedAgent          Agent
	agentViewMode          viewMode
	agentFormActive        bool
	agentForm              *huh.Form
	agentAction            string
	agentToDelete          string
	currentEditingAgent    Agent
	availableModelVersions []string
	modelsFetchError       error
	errorMessage           string
	availableTools         []Tool
	toolUsages             []ToolUsage
	toolUsageFilePath      string
	chats                  []Chat
	chatList               list.Model
	selectedChat           *Chat
	chatsFolderPath        string
	newChatForm            *huh.Form
	newChatName            string
	newProjectName         string
	filePicker             filepicker.Model
	selectedImage          string
}

type OllamaModel struct {
	Name       string    `json:"name"`
	Model      string    `json:"model"`
	ModifiedAt time.Time `json:"modified_at"`
	Size       int64     `json:"size"`
	Details    struct {
		ParameterSize     string `json:"parameter_size"`
		QuantizationLevel string `json:"quantization_level"`
	} `json:"details"`
}

type AvailableModel struct {
	Name  string   `json:"name"`
	Sizes []string `json:"sizes"`
}

type PullResponse struct {
	Status    string  `json:"status"`
	Digest    string  `json:"digest,omitempty"`
	Total     int64   `json:"total,omitempty"`
	Completed int64   `json:"completed,omitempty"`
	Progress  float64 `json:"progress,omitempty"`
}

type ChatConfig struct {
	ModelVersion    string
	SystemPrompt    string
	ContextFilePath string
	Tokens          string
}

type Chat struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	ProjectName string              `json:"project_name"`
	CreatedAt   time.Time           `json:"created_at"`
	Messages    []map[string]string `json:"messages"`
}
type chatItem struct {
	chat Chat
}

type chatDelegate struct {
	styles struct {
		normal, selected lipgloss.Style
	}
}

type (
	responseMsg        string
	errMsg             error
	modelsMsg          []OllamaModel
	availableModelsMsg []AvailableModel
	modelDeletedMsg    struct{}
	modelDownloadedMsg string
	scrapeCompletedMsg struct{}
	agentsMsg          []Agent
	notifyMsg          string
	OllamaToggledMsg   struct{}
)

type agentDeletedMsg struct {
	Role string
}

type agentUpdatedMsg struct {
	Role string
}

type initialTransitionMsg struct{}

type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]interface{} `json:"parameters"`
}

type ToolUsage struct {
	Timestamp    time.Time `json:"timestamp"`
	AgentRole    string    `json:"agent_role"`
	ToolName     string    `json:"tool_name"`
	Input        string    `json:"input,omitempty"`
	Output       string    `json:"output"`
	Success      bool      `json:"success"`
	ErrorMessage string    `json:"error_message,omitempty"`
}

type ToolCall struct {
	Name       string            `json:"name"`
	Parameters map[string]string `json:"parameters"`
}

type Agent struct {
	Role            string   `json:"role"`
	ModelVersion    string   `json:"model_version"`
	SystemPrompt    string   `json:"system_prompt"`
	UseContext      bool     `json:"use_context"`
	ContextFilePath string   `json:"context_file_path"`
	UseConversation bool     `json:"use_conversation"`
	Tokens          string   `json:"tokens"`
	Tools           []Tool   `json:"tools,omitempty"`
	SelectedTools   []string `json:"selected_tools,omitempty"`
}
