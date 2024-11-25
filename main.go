package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"go/format"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
)

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

func (i chatItem) FilterValue() string {
	return i.chat.Name
}

func (i chatItem) Title() string {
	return i.chat.Name
}

var docStyle = lipgloss.NewStyle().Margin(1, 2)

func (i chatItem) Description() string {
	return fmt.Sprintf("Project: %s | Created: %s | Messages: %d",
		i.chat.ProjectName,
		i.chat.CreatedAt.Format("2006-01-02 15:04:05"),
		len(i.chat.Messages))
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
	ChatListView viewMode = iota
	NewChatFormView
)

const (
	defaultSystemPrompt     = "You are an assistant tasked with generating code based on the user's prompt. Use the following context to generate the best solution. Context: {context}"
	defaultContextFilePath  = ""
	defaultTokens           = "16384"
	defaultModelVersion     = "llama3.1"
	ollamaAPIURL            = "http://localhost:11434/api"
	defaultIndicatorPrompt  = "│"
	configFormTitle         = "Chat Configuration"
	agentFormTitle          = "Agent Configuration"
	confirmDeleteAgentTitle = "Confirm Agent Deletion"
	confirmDeleteModelTitle = "Confirm Model Deletion"
	agentsFilePath          = "./agents.json" // Path to the agents JSON file
)

var (
	runningIndicatorColor = lipgloss.Color("#00FF00")
	stoppedIndicatorColor = lipgloss.Color("#FF0000")
	errorStyle            = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FF0000")).
				Bold(true)
)

func (m *model) indicatorStyle() lipgloss.Style {
	var color lipgloss.Color
	if m.ollamaRunning {
		color = runningIndicatorColor
	} else {
		color = stoppedIndicatorColor
	}

	return lipgloss.NewStyle().
		Foreground(color).
		Background(lipgloss.Color("#000000")).
		Border(lipgloss.HiddenBorder()).
		Padding(0)
}

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
}

type ChatConfig struct {
	ModelVersion    string
	SystemPrompt    string
	ContextFilePath string
	Tokens          string
}

func loadFileContext(filePath string) (string, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", filePath, err)
	}
	return string(content), nil
}

func saveAgents(m *model) error {
	data, err := json.MarshalIndent(m.agents, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal agents: %w", err)
	}

	err = os.WriteFile(agentsFilePath, data, 0644)
	if err != nil {
		return fmt.Errorf("failed to write agents to file: %w", err)
	}

	return nil
}

func loadAgents(m *model) error {
	if _, err := os.Stat(agentsFilePath); os.IsNotExist(err) {
		return nil
	}

	data, err := os.ReadFile(agentsFilePath)
	if err != nil {
		return fmt.Errorf("failed to read agents file: %w", err)
	}

	var loadedAgents []Agent
	err = json.Unmarshal(data, &loadedAgents)
	if err != nil {
		return fmt.Errorf("failed to unmarshal agents: %w", err)
	}

	m.agents = loadedAgents

	return nil
}

func newChatDelegate() chatDelegate {
	d := chatDelegate{}

	d.styles.normal = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Padding(0, 0, 0, 2).
		MarginBottom(1) // Add consistent bottom margin

	d.styles.selected = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#000000")).
		Background(lipgloss.Color("#00FF00")).
		Padding(0, 0, 0, 2).
		MarginBottom(1) // Add consistent bottom margin

	return d
}

// Height returns the fixed height for each list item
func (d chatDelegate) Height() int {
	return 3 // Changed from 3 to 2: one line for title, one for description
}

// Spacing returns zero to let the styles handle spacing
func (d chatDelegate) Spacing() int {
	return 0
}

// Update implements list.ItemDelegate
func (d chatDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd {
	return nil
}

// Render implements list.ItemDelegate
func (d chatDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(chatItem)
	if !ok {
		return
	}

	// Create the display strings
	title := i.Title()
	desc := i.Description()

	// Build the final string with exactly two lines
	str := fmt.Sprintf("%s\n%s", title, desc)

	// Apply the appropriate style
	fn := d.styles.normal.Render
	if index == m.Index() {
		fn = d.styles.selected.Render
	}

	// Write the styled string
	fmt.Fprint(w, fn(str))
}

func createNewChatForm(name *string, projectName *string) *huh.Form {
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Chat Name").
				Placeholder("Enter a name for the new chat").
				Value(name).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return fmt.Errorf("chat name cannot be empty")
					}
					if len(s) > 50 {
						return fmt.Errorf("chat name too long (max 50 characters)")
					}
					return nil
				}),

			huh.NewInput().
				Title("Project Name").
				Placeholder("Enter the project name").
				Value(projectName).
				Validate(func(s string) error {
					if strings.TrimSpace(s) == "" {
						return fmt.Errorf("project name cannot be empty")
					}
					if len(s) > 100 {
						return fmt.Errorf("project name too long (max 100 characters)")
					}
					return nil
				}),
		),
	).WithShowHelp(true)
	form.NextField()
	form.PrevField()
	return form
}

// Initialize chat list component
func (m *model) initializeChatList() error {
	// Create chats directory if it doesn't exist
	if err := os.MkdirAll(m.chatsFolderPath, 0755); err != nil {
		return fmt.Errorf("failed to create chats directory: %w", err)
	}

	// Load existing chats
	chats, err := loadChats(m.chatsFolderPath)
	if err != nil {
		return fmt.Errorf("failed to load chats: %w", err)
	}

	// Convert chats to list items
	items := make([]list.Item, 0, len(chats)+1)
	items = append(items, chatItem{Chat{Name: "Create New Chat", ProjectName: ""}})
	for _, chat := range chats {
		items = append(items, chatItem{chat})
	}

	delegate := newChatDelegate()
	m.chatList = list.New(items, delegate, m.width, m.height-4) // Set initial size using full height
	m.chatList.Title = "Chat List"
	m.chatList.SetShowStatusBar(false)
	m.chatList.SetFilteringEnabled(true)
	m.chatList.Styles.Title = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#666666")).
		Padding(0, 1)

	// Set additional styles for better visibility
	m.chatList.Styles.NoItems = lipgloss.NewStyle().Margin(1, 2)
	m.chatList.SetSize(m.width, m.height-4) // Set size again to ensure proper layout

	return nil
}

func triggerWindowResize(width, height int) tea.Cmd {
	return func() tea.Msg {
		return tea.WindowSizeMsg{
			Width:  width,
			Height: height,
		}
	}
}

// Update the model to handle chat list interactions
func (m *model) updateChatList(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		headerHeight := 3 // Account for header and padding
		m.chatList.SetSize(msg.Width-2, msg.Height-headerHeight)
		return m, nil

	case tea.KeyMsg:
		switch keypress := msg.String(); keypress {
		case "up", "k":
			if m.chatList.Index() > 0 {
				m.chatList.CursorUp()
			}
			return m, nil

		case "down", "j":
			if m.chatList.Index() < len(m.chatList.Items())-1 {
				m.chatList.CursorDown()
			}
			return m, nil

		case "enter":
			selectedItem := m.chatList.SelectedItem()
			if selectedItem == nil {
				return m, nil
			}

			if chatItem, ok := selectedItem.(chatItem); ok {
				if chatItem.chat.Name == "Create New Chat" {
					m.viewMode = NewChatFormView
					m.formActive = true
					m.newChatName = ""
					m.newProjectName = ""
					m.newChatForm = createNewChatForm(&m.newChatName, &m.newProjectName)
					return m, nil
				}

				// Load and switch to the selected chat
				m.selectedChat = &chatItem.chat
				m.conversationHistory = chatItem.chat.Messages
				m.viewMode = ChatView
				m.updateViewport()
				return m, nil
			}
		}
	}

	// For other messages, pass them to the chatList's update function
	m.chatList, cmd = m.chatList.Update(msg)
	return m, cmd
}

func (m *model) createNewChat(name string, projectName string) error {
	chat := createNewChat(name, projectName)

	// Save the new chat
	if err := saveChat(chat, m.chatsFolderPath); err != nil {
		return fmt.Errorf("failed to save new chat: %w", err)
	}

	// Add to list
	m.chatList.InsertItem(1, chatItem{chat})

	// Select and load the new chat
	m.selectedChat = &chat
	m.conversationHistory = []map[string]string{}
	m.viewMode = ChatView

	return nil
}

// TODO REMOVE
func createConfigForm(config *ChatConfig, modelVersions []string) *huh.Form {
	modelOptions := make([]huh.Option[string], 0, len(modelVersions))
	for _, mv := range modelVersions {
		modelOptions = append(modelOptions, huh.NewOption(mv, mv))
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Model Version").
				Options(modelOptions...).
				Value(&config.ModelVersion),

			huh.NewInput().
				Title("System Prompt").
				Value(&config.SystemPrompt),

			huh.NewInput().
				Title("Context File Path").
				Value(&config.ContextFilePath),

			huh.NewInput().
				Title("Input Tokens").
				Value(&config.Tokens),
		),
	).WithShowHelp(true)
	return form
}

func createAgentForm(agent *Agent, modelVersions []string, availableTools []Tool) *huh.Form {
	if agent.SelectedTools == nil {
		agent.SelectedTools = []string{}
	}

	modelOptions := make([]huh.Option[string], 0, len(modelVersions))
	for _, mv := range modelVersions {
		modelOptions = append(modelOptions, huh.NewOption(mv, mv))
	}

	toolOptions := make([]huh.Option[string], 0, len(availableTools))
	for _, tool := range availableTools {
		toolOptions = append(toolOptions, huh.NewOption(tool.Name, tool.Name))
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Role").
				Placeholder("Enter a unique role identifier").
				Value(&agent.Role),

			huh.NewSelect[string]().
				Title("Model Version").
				Options(modelOptions...).
				Value(&agent.ModelVersion),

			huh.NewText().
				Title("System Prompt").
				Value(&agent.SystemPrompt),

			huh.NewSelect[bool]().
				Title("Use Context File").
				Options(
					huh.NewOption("Yes", true),
					huh.NewOption("No", false),
				).
				Value(&agent.UseContext),

			huh.NewInput().
				Value(&agent.ContextFilePath).
				TitleFunc(func() string {
					if agent.UseContext {
						return "Context File Path"
					}
					return "Context Status"
				}, &agent.UseContext).
				PlaceholderFunc(func() string {
					if agent.UseContext {
						return "/path/to/your/context/file"
					}
					return "No context file selected"
				}, &agent.UseContext).
				Validate(func(s string) error {
					if !agent.UseContext {
						return nil
					}
					if s == "" {
						return fmt.Errorf("context file path is required when context is enabled")
					}
					if _, err := os.Stat(s); err != nil {
						return fmt.Errorf("file not found: %s", s)
					}
					return nil
				}),

			huh.NewSelect[bool]().
				Title("Use Conversation History").
				Options(
					huh.NewOption("Yes", true),
					huh.NewOption("No", false),
				).
				Value(&agent.UseConversation),

			huh.NewInput().
				Title("Tokens").
				Placeholder("16384").
				Value(&agent.Tokens),

			huh.NewMultiSelect[string]().
				Title("Tools").
				Options(toolOptions...).
				Value(&agent.SelectedTools),
		),
	).WithShowHelp(true)
	form.NextField()
	form.PrevField()

	return form
}

func createConfirmForm(title string, confirmResult *bool) *huh.Form {
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(title).
				Affirmative("Yes").
				Negative("No").
				Value(confirmResult),
		),
	).WithShowHelp(false)
	return form
}

func setupTextarea() textarea.Model {
	ta := textarea.New()
	ta.Placeholder = "Ask something..."
	ta.Focus()
	ta.CharLimit = 0
	ta.SetWidth(50)
	ta.SetHeight(3)

	indicatorStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Render(defaultIndicatorPrompt)

	ta.Prompt = indicatorStyle
	return ta
}

func InitialModel() *model {
	ta := setupTextarea()
	vp := viewport.New(85, 20)
	renderer, _ := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(vp.Width),
	)

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#00FF00"))

	tableStyle := table.DefaultStyles()
	tableStyle.Header = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF"))
	tableStyle.Selected = lipgloss.NewStyle().Foreground(lipgloss.Color("#000000")).Background(lipgloss.Color("#00FF00"))

	modelColumns := []table.Column{
		{Title: "Name", Width: 30},
		{Title: "Parameter Size", Width: 15},
		{Title: "Size (GB)", Width: 10},
	}

	modelTable := table.New(
		table.WithColumns(modelColumns),
		table.WithFocused(false), // Initially unfocused
		table.WithStyles(tableStyle),
	)

	availableColumns := []table.Column{
		{Title: "Available Models", Width: 30},
		{Title: "Sizes", Width: 20},
	}

	availableTable := table.New(
		table.WithColumns(availableColumns),
		table.WithFocused(false), // Initially unfocused
		table.WithStyles(tableStyle),
	)

	parameterSizesTable := table.New(
		table.WithColumns([]table.Column{
			{Title: "Available Sizes", Width: 20},
		}),
		table.WithFocused(false),
		table.WithStyles(tableStyle),
	)

	agentColumns := []table.Column{
		{Title: "Role", Width: 30},
		{Title: "Model Version", Width: 15},
	}

	agentsTable := table.New(
		table.WithColumns(agentColumns),
		table.WithFocused(false),
		table.WithStyles(tableStyle),
	)

	// Define available tools
	availableTools := []Tool{
		checkGoCodeTool,
		// Add more tools here if needed
	}

	// Assign availableTools to the model's field
	m := &model{
		userMessages:        make([]string, 0),
		assistantResponses:  make([]string, 0),
		conversationHistory: []map[string]string{},
		currentUserMessage:  "",
		textarea:            ta,
		viewport:            vp,
		modelTable:          modelTable,
		availableTable:      availableTable,
		parameterSizesTable: parameterSizesTable,
		spinner:             sp,
		renderer:            renderer,
		viewMode:            ChatView,
		ollamaRunning:       false,
		config: ChatConfig{
			ModelVersion:    defaultModelVersion,
			SystemPrompt:    defaultSystemPrompt,
			ContextFilePath: defaultContextFilePath,
			Tokens:          defaultTokens,
		},
		formActive:             false,
		agents:                 []Agent{},
		agentsTable:            agentsTable,
		agentViewMode:          ChatView,
		agentFormActive:        false,
		availableTools:         availableTools, // Assigned here
		availableModelVersions: []string{},
		modelsFetchError:       nil,
		errorMessage:           "",
		confirmDeleteType:      "",
		toolUsages:             []ToolUsage{}, // Initialize as empty
		toolUsageFilePath:      "./tool_usages.json",
	}

	err := loadAgents(m)
	if err != nil {
		log.Printf("Error loading agents from file: %v", err)
		m.agents = append(m.agents, Agent{
			Role:            "Assistant",
			ModelVersion:    "llama3.1",
			SystemPrompt:    "You are an assistant tasked with generating code based on the user's prompt.",
			UseContext:      false,
			ContextFilePath: "",
			UseConversation: false, // Default to not using conversation history
			Tokens:          "16384",
		}, Agent{
			Role:            "Tester",
			ModelVersion:    "llama3.1",
			SystemPrompt:    "You are a code tester tasked with reviewing the following code for potential bugs or issues.",
			UseContext:      false,
			ContextFilePath: "",
			UseConversation: true, // Example of enabling conversation history by default
			Tokens:          "16384",
		})

		err = saveAgents(m)
		if err != nil {
			log.Printf("Failed to save default agents: %v", err)
		}
		err := loadToolUsages(m)
		if err != nil {
			log.Printf("Error loading tool usages: %v", err)
		}
	}

	m.populateAgentsTable()

	// Pass m.availableTools to createAgentForm
	m.agentForm = createAgentForm(&m.currentEditingAgent, m.availableModelVersions, m.availableTools)

	m.availableModelVersions = []string{defaultModelVersion}

	m.configForm = createConfigForm(&m.config, m.availableModelVersions)

	m.updateTextareaIndicatorColor()

	m.chatsFolderPath = "./chats"
	if err = m.initializeChatList(); err != nil {
		log.Printf("Error initializing chat list: %v", err)
	}
	m.newChatName = ""
	m.newProjectName = ""
	m.newChatForm = createNewChatForm(&m.newChatName, &m.newProjectName)

	return m
}

func loadChats(folderPath string) ([]Chat, error) {
	// Create chats directory if it doesn't exist
	if err := os.MkdirAll(folderPath, 0755); err != nil {
		return nil, fmt.Errorf("failed to create chats directory: %w", err)
	}

	var chats []Chat
	files, err := os.ReadDir(folderPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read chats directory: %w", err)
	}

	for _, file := range files {
		if !file.IsDir() && strings.HasSuffix(file.Name(), ".json") {
			data, err := os.ReadFile(filepath.Join(folderPath, file.Name()))
			if err != nil {
				continue
			}

			var chat Chat
			if err := json.Unmarshal(data, &chat); err != nil {
				continue
			}
			chats = append(chats, chat)
		}
	}

	// Sort chats by creation time, newest first
	sort.Slice(chats, func(i, j int) bool {
		return chats[i].CreatedAt.After(chats[j].CreatedAt)
	})

	return chats, nil
}

func saveChat(chat Chat, folderPath string) error {
	data, err := json.MarshalIndent(chat, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal chat: %w", err)
	}

	filename := filepath.Join(folderPath, chat.ID+".json")
	if err := os.WriteFile(filename, data, 0644); err != nil {
		return fmt.Errorf("failed to write chat file: %w", err)
	}

	return nil
}

func createNewChat(name string, projectName string) Chat {
	return Chat{
		ID:          uuid.New().String(),
		Name:        name,
		ProjectName: projectName,
		CreatedAt:   time.Now(),
		Messages:    make([]map[string]string, 0),
	}
}

func (m *model) handleChatSelection(chat *Chat) {
	m.selectedChat = chat
	m.conversationHistory = chat.Messages
	m.viewMode = ChatView
	m.updateViewport()
}

func loadToolUsages(m *model) error {
	if _, err := os.Stat(m.toolUsageFilePath); os.IsNotExist(err) {
		// File does not exist; initialize with empty slice
		m.toolUsages = []ToolUsage{}
		return nil
	}

	data, err := ioutil.ReadFile(m.toolUsageFilePath)
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

func (m *model) populateAgentsTable() {
	var rows []table.Row

	rows = append(rows, table.Row{"Add New Agent"})

	for _, agent := range m.agents {
		rows = append(rows, table.Row{
			agent.Role,
			agent.ModelVersion,
		})
	}

	m.agentsTable.SetRows(rows)

	if m.viewMode == AgentView && m.agentsTable.Focused() && len(rows) > 0 {
		m.agentsTable.SetCursor(0)
	}
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(
		textarea.Blink,
		tea.EnterAltScreen,
		fetchModelsCmd(),
		m.spinner.Tick,
	)
}

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

func (m *model) updateTextareaIndicatorColor() {
	if m.ollamaRunning {
		m.textarea.Prompt = lipgloss.NewStyle().
			Foreground(runningIndicatorColor).
			Render(defaultIndicatorPrompt)
	} else {
		m.textarea.Prompt = lipgloss.NewStyle().
			Foreground(stoppedIndicatorColor).
			Render(defaultIndicatorPrompt)
	}
}

func (m *model) navigate(direction string) {
	switch m.viewMode {
	case ChatListView:
		if direction == "up" {
			if m.chatList.Index() > 0 {
				m.chatList.CursorUp()
			}
		} else if direction == "down" {
			if m.chatList.Index() < len(m.chatList.Items())-1 {
				m.chatList.CursorDown()
			}
		}
	case ModelView:
		if direction == "up" {
			m.modelTable.MoveUp(1)
		} else if direction == "down" {
			m.modelTable.MoveDown(1)
		}
	case AvailableModelsView:
		if direction == "up" {
			m.availableTable.MoveUp(1)
		} else if direction == "down" {
			m.availableTable.MoveDown(1)
		}
	case ParameterSizesView:
		if direction == "up" {
			m.parameterSizesTable.MoveUp(1)
		} else if direction == "down" {
			m.parameterSizesTable.MoveDown(1)
		}
	case AgentView:
		if direction == "up" {
			m.agentsTable.MoveUp(1)
		} else if direction == "down" {
			m.agentsTable.MoveDown(1)
		}
	case ChatView:
		if direction == "up" {
			m.viewport.LineUp(1)
		} else if direction == "down" {
			m.viewport.LineDown(1)
		}
	}
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	// Handle chat list view first
	if m.viewMode == ChatListView {
		return m.updateChatList(msg)
	}

	// Handle error messages
	if m.errorMessage != "" {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "esc", "q":
				m.errorMessage = ""
				return m, nil
			case "r":
				// Retry logic if applicable
				m.errorMessage = ""
				return m, fetchModelsCmd()
			}
		default:
			return m, nil
		}
	}

	// Global key handling
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if keyIsCtrlZ(msg) {
			return m, tea.Quit
		}

		if msg.String() == "esc" {
			if m.formActive {
				m.formActive = false
				m.viewMode = ChatView
				m.textarea.Focus()
				return m, nil
			}
			if m.agentFormActive {
				m.agentFormActive = false
				m.viewMode = AgentView
				m.agentsTable.Focus()
				return m, nil
			}
			if m.confirmForm != nil {
				m.viewMode = (func() viewMode {
					if m.confirmDeleteType == "model" {
						return ModelView
					}
					return AgentView
				})()
				m.confirmDeleteModelName = ""
				m.agentToDelete = ""
				m.confirmDeleteType = ""
				m.confirmForm = nil
				switch m.viewMode {
				case ModelView:
					m.modelTable.Focus()
				case AgentView:
					m.agentsTable.Focus()
				}
				return m, nil
			}
			m.viewMode = ChatView
			m.formActive = false
			m.agentFormActive = false
			m.textarea.Focus()
			return m, nil
		}
	}

	// Centralized form handling
	if m.formActive {
		var updatedForm interface{}
		var formCmd tea.Cmd

		switch m.viewMode {
		case NewChatFormView:
			updatedForm, formCmd = m.newChatForm.Update(msg)
			m.newChatForm = updatedForm.(*huh.Form)
		case AgentFormView:
			updatedForm, formCmd = m.agentForm.Update(msg)
			m.agentForm = updatedForm.(*huh.Form)
		default:
			updatedForm, formCmd = m.configForm.Update(msg)
			m.configForm = updatedForm.(*huh.Form)
		}

		// Handle form completion
		switch m.viewMode {
		case NewChatFormView:
			if m.newChatForm.State == huh.StateCompleted {
				if m.newChatName == "" {
					m.errorMessage = "Chat name cannot be empty"
					m.newChatForm.State = huh.StateNormal
					return m, nil
				}
				if m.newProjectName == "" {
					m.errorMessage = "Project name cannot be empty"
					m.newChatForm.State = huh.StateNormal
					return m, nil
				}

				// Create new chat with both name and project name
				err := m.createNewChat(m.newChatName, m.newProjectName)
				if err != nil {
					m.errorMessage = fmt.Sprintf("Failed to create new chat: %v", err)
					return m, nil
				}

				// Reset form and names for next use
				m.newChatName = ""
				m.newProjectName = ""
				m.newChatForm = createNewChatForm(&m.newChatName, &m.newProjectName)

				// Switch back to ChatView
				m.viewMode = ChatView
				m.formActive = false
				m.updateViewport()
				return m, nil
			}
			// Handle other forms if necessary
		}
		return m, formCmd
	}

	// Handle agent form when active
	if m.agentFormActive {
		updatedForm, formCmd := m.agentForm.Update(msg)
		m.agentForm = updatedForm.(*huh.Form)

		switch m.agentForm.State {
		case huh.StateCompleted:
			// Clear existing tools
			m.currentEditingAgent.Tools = []Tool{}

			// Map selected tool names to Tool structs
			for _, toolName := range m.currentEditingAgent.SelectedTools {
				for _, availableTool := range m.availableTools {
					if availableTool.Name == toolName {
						m.currentEditingAgent.Tools = append(m.currentEditingAgent.Tools, availableTool)
						break
					}
				}
			}

			// Add or edit the agent
			if m.agentAction == "add" {
				m.agents = append(m.agents, m.currentEditingAgent)
				log.Printf("Added new agent with role: %s\n", m.currentEditingAgent.Role)
			} else if m.agentAction == "edit" {
				for i, agent := range m.agents {
					if strings.EqualFold(agent.Role, m.selectedAgent.Role) {
						m.agents[i] = m.currentEditingAgent
						log.Printf("Edited agent with role: %s\n", m.currentEditingAgent.Role)
						break
					}
				}
			}

			m.agentFormActive = false
			m.viewMode = AgentView
			m.populateAgentsTable()
			m.agentsTable.Focus()

			err := saveAgents(m)
			if err != nil {
				m.errorMessage = fmt.Sprintf("Failed to save agents: %v", err)
				return m, nil
			}

			return m, nil
		}

		return m, formCmd
	}

	// Handle confirmation form
	if m.viewMode == ConfirmDelete && m.confirmForm != nil {
		updatedConfirmForm, confirmCmd := m.confirmForm.Update(msg)
		m.confirmForm = updatedConfirmForm.(*huh.Form)

		switch m.confirmForm.State {
		case huh.StateCompleted:
			var cmds []tea.Cmd
			if m.confirmResult {
				if m.confirmDeleteType == "model" {
					log.Printf("User confirmed deletion of model: %s\n", m.confirmDeleteModelName)
					cmds = append(cmds, deleteModelCmd(m.confirmDeleteModelName))
				} else if m.confirmDeleteType == "agent" {
					log.Printf("User confirmed deletion of agent: %s\n", m.agentToDelete)
					cmds = append(cmds, deleteAgentCmd(m.agentToDelete))
				}
			} else {
				log.Printf("User canceled deletion of %s: %s\n", m.confirmDeleteType, m.confirmDeleteModelName)
			}

			previousView := ModelView
			if m.confirmDeleteType == "agent" {
				previousView = AgentView
			}
			m.viewMode = previousView
			m.confirmDeleteModelName = ""
			m.agentToDelete = ""
			m.confirmDeleteType = ""
			m.confirmForm = nil

			switch m.viewMode {
			case ModelView:
				m.modelTable.Focus()
			case AgentView:
				m.agentsTable.Focus()
			}

			return m, tea.Batch(cmds...)
		}

		return m, confirmCmd
	}

	// Handle other messages and view modes
	switch msg := msg.(type) {
	case notifyMsg:
		m.errorMessage = string(msg)
		return m, nil

	case tea.KeyMsg:
		switch {
		case keyIsCtrlZ(msg):
			return m, tea.Quit
		}

		if m.viewMode == InsertView && msg.Type == tea.KeyEnter {
			return m.handleEnterKey()
		}

		if m.viewMode == InsertView {
			m.textarea, cmd = m.textarea.Update(msg)
			return m, cmd
		}

		switch msg.String() {
		case "o":
			if m.viewMode == ChatView || m.viewMode == ModelView {
				return m, m.toggleOllamaServe()
			}
			return m, nil
		case "m":
			if m.viewMode == ChatView {
				m.viewMode = ModelView
				m.modelTable.Focus()
				m.textarea.Blur()
				m.availableTable.Blur()
				m.agentsTable.Blur()
				m.parameterSizesTable.Blur()
				return m, fetchModelsCmd()
			}
			return m, nil
		case "i":
			if m.viewMode == ChatView {
				m.viewMode = InsertView
				m.textarea.Focus()
				m.modelTable.Blur()
				m.availableTable.Blur()
				m.agentsTable.Blur()
				m.parameterSizesTable.Blur()
				return m, nil
			}
		case "g":
			if m.viewMode != AgentView {
				m.viewMode = AgentView
				m.agentsTable.Focus()
				m.modelTable.Blur()
				m.availableTable.Blur()
				m.parameterSizesTable.Blur()
				return m, nil
			}
		case "l":
			if m.viewMode == ChatView {
				m.viewMode = ChatListView
				return m, triggerWindowResize(m.width, m.height) // Add this resize trigger
			}
		case "a":
			if m.viewMode == AgentView {
				m.agentAction = "add"
				m.currentEditingAgent = Agent{}
				m.agentForm = createAgentForm(&m.currentEditingAgent, m.availableModelVersions, m.availableTools)
				m.agentFormActive = true
				m.viewMode = AgentFormView
				m.agentsTable.Blur()
				return m, nil
			}
		case "e":
			if m.viewMode == AgentView {
				selectedRow := m.agentsTable.SelectedRow()
				if selectedRow == nil || selectedRow[0] == "Add New Agent" {
					return m, nil
				}
				agentRole := selectedRow[0]
				for _, agent := range m.agents {
					if strings.EqualFold(agent.Role, agentRole) {
						m.selectedAgent = agent
						m.currentEditingAgent = agent
						break
					}
				}
				m.agentAction = "edit"
				m.agentForm = createAgentForm(&m.currentEditingAgent, m.availableModelVersions, m.availableTools)
				m.viewMode = AgentFormView
				m.agentsTable.Blur()
				return m, nil
			}
		case "d":
			if m.viewMode == AgentView {
				selectedRow := m.agentsTable.SelectedRow()
				if selectedRow == nil || selectedRow[0] == "Add New Agent" {
					return m, nil
				}
				m.agentToDelete = selectedRow[0]
				m.confirmDeleteType = "agent"
				m.confirmForm = createConfirmForm(fmt.Sprintf("Are you sure you want to delete agent '%s'? This action cannot be undone.", m.agentToDelete), &m.confirmResult)
				m.viewMode = ConfirmDelete
				m.agentsTable.Blur()
				return m, nil
			}
			if m.viewMode == ModelView {
				selectedRow := m.modelTable.SelectedRow()
				if selectedRow == nil || selectedRow[0] == "Add New Model" {
					return m, nil
				}
				modelName := selectedRow[0]
				m.confirmDeleteModelName = modelName
				m.confirmDeleteType = "model"
				m.confirmForm = createConfirmForm(fmt.Sprintf("Are you sure you want to delete model '%s'? This action cannot be undone.", modelName), &m.confirmResult)
				m.viewMode = ConfirmDelete
				m.modelTable.Blur()
				return m, nil
			}
		case "u":
			if m.viewMode == AgentView {
				m.moveAgentUp()
				return m, saveAgentsCmd(m)
			}
		case "y":
			if m.viewMode == AgentView {
				m.moveAgentDown()
				return m, saveAgentsCmd(m)
			}
		case "esc":
			switch m.viewMode {
			case AgentFormView:
				m.viewMode = AgentView
				m.agentFormActive = false
				m.agentsTable.Focus()
				return m, nil
			case ConfirmDelete:
				m.viewMode = (func() viewMode {
					if m.confirmDeleteType == "model" {
						return ModelView
					}
					return AgentView
				})()
				m.confirmDeleteModelName = ""
				m.agentToDelete = ""
				m.confirmDeleteType = ""
				m.confirmForm = nil
				switch m.viewMode {
				case ModelView:
					m.modelTable.Focus()
				case AgentView:
					m.agentsTable.Focus()
				}
				return m, nil
			default:
				m.viewMode = ChatView
				m.formActive = false
				m.agentFormActive = false
				m.textarea.Focus()
				return m, nil
			}
		case "enter":
			return m.handleEnterKey()
		case "j", "down":
			m.navigate("down")
		case "k", "up":
			m.navigate("up")
		}

	case modelsMsg:
		if len(msg) == 0 {
			log.Println("No models available to populate the model table.")
			return m, nil
		}

		m.populateModelTable(msg)

		m.availableModelVersions = make([]string, len(msg))
		for i, mdl := range msg {
			m.availableModelVersions[i] = mdl.Model
		}

		return m, nil

	case availableModelsMsg:
		m.availableModels = msg
		m.populateAvailableModelsTable(msg)

	case modelDeletedMsg:
		m.viewMode = ModelView
		m.modelTable.Focus()
		return m, fetchModelsCmd()

	case modelDownloadedMsg:
		m.viewMode = ModelView
		m.modelTable.Focus()
		m.availableTable.Blur()
		m.agentsTable.Blur()
		m.parameterSizesTable.Blur()
		return m, fetchModelsCmd()

	case agentsMsg:
		m.agents = msg
		m.populateAgentsTable()
		m.agentsTable.Focus()
		return m, nil

	case agentDeletedMsg:
		for i, agent := range m.agents {
			if strings.EqualFold(agent.Role, msg.Role) {
				m.agents = append(m.agents[:i], m.agents[i+1:]...)
				break
			}
		}
		log.Printf("Agent with role '%s' deleted successfully.\n", msg.Role)
		m.agentToDelete = ""
		m.populateAgentsTable()
		m.agentsTable.Focus()

		err := saveAgents(m)
		if err != nil {
			m.errorMessage = fmt.Sprintf("Failed to save agents: %v", err)
			return m, nil
		}

		return m, nil

	case errMsg:
		m.loading = false
		m.errorMessage = msg.Error()
		return m, nil

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.textarea.SetWidth(m.width)
		m.viewport.Width = m.width
		m.viewport.Height = m.height - 3
		m.updateViewport()

		m.availableTable.SetWidth(m.width)
		m.availableTable.SetHeight(m.height - 4)
		m.parameterSizesTable.SetWidth(m.width)
		m.parameterSizesTable.SetHeight(m.height - 4)
		m.agentsTable.SetWidth(m.width)

		// Update chat list size to use full height
		if m.viewMode == ChatListView {
			headerHeight := 2 // Account for header and padding
			m.chatList.SetSize(msg.Width-2, msg.Height-headerHeight)
		}

		switch m.viewMode {
		case AgentView:
			availableHeight := m.height - 4
			if availableHeight < 3 {
				availableHeight = 3
			}
			m.agentsTable.SetHeight(availableHeight)
		case ChatListView:
			return m.updateChatList(msg)
		default:
			m.agentsTable.SetHeight(m.height - 4)
		}

		return m, nil

	case spinner.TickMsg:
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case responseMsg:
	case OllamaToggledMsg:
		m.ollamaRunning = !m.ollamaRunning
		m.updateTextareaIndicatorColor()
		return m, nil
	}

	// Handle chat list view
	// Updated code
	if m.viewMode == ChatListView {
		return m.updateChatList(msg)
	}

	return m, cmd
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

	// Check if this agent has the code checker tool and if there's code to check
	hasCodeChecker := false
	for _, tool := range agent.Tools {
		if tool.Name == "check_go_code" {
			hasCodeChecker = true
			break
		}
	}
	codeBlocks := extractCodeBlocks(input)
	hasCode := len(codeBlocks) > 0

	// Create system prompt based on agent configuration
	var systemPrompt string
	if hasCodeChecker && hasCode {
		systemPrompt = `You are a code review assistant. Your primary task is to analyze and test Go code.
Follow these steps for each code review:

1. Examine the provided code carefully
2. Use the check_go_code tool to analyze it
    - you will ALWAYS use this tool on go code
    - print any errors or warnings you get
3. Analyze the tool's output thoroughly:
   - Build errors indicate the code won't compile
   - Linter warnings suggest potential issues
   - Pay special attention to type errors and undefined variables
4. Always provide:
   - A clear summary of all issues found
   - Specific suggestions for fixing each problem
   - Example corrections where appropriate
5. Even if the code passes checks, consider:
   - Code organization
   - Error handling
   - Best practices
   - Performance implications

Important: Always use the check_go_code tool on any Go code you receive. Do not skip this step.`

		if contextContent != "" {
			systemPrompt = fmt.Sprintf("%s\n\nContext: %s", systemPrompt, contextContent)
		}
	} else {
		if contextContent != "" {
			systemPrompt = strings.ReplaceAll(agent.SystemPrompt, "{context}", contextContent)
		} else {
			systemPrompt = strings.ReplaceAll(agent.SystemPrompt, "{context}", "")
		}
	}

	// Prepare messages for the agent
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

	// Prepare the API request
	payload := map[string]interface{}{
		"model":    agent.ModelVersion,
		"messages": messages,
		"stream":   false,
	}

	// Add tools if needed
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

	// Send request to Ollama API
	resp, err := http.Post(ollamaAPIURL+"/chat", "application/json", bytes.NewBuffer(requestBody))
	if err != nil {
		return "", fmt.Errorf("failed to send request to Ollama API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
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

	// Only add "Initial Analysis" header when using code checker
	if hasCodeChecker && hasCode {
		if !strings.Contains(apiResponse.Message.Content, `{"name": "check_go_code"`) {
			fullResponse.WriteString("Initial Analysis:\n")
		}
	}
	fullResponse.WriteString(apiResponse.Message.Content)

	// Process tool calls if any
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
					// Send the lint results back to the agent for analysis
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

					// Make a second API call for analysis of the lint results
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

	// Add the final response to conversation history
	m.conversationHistory = append(m.conversationHistory, map[string]string{
		"role":    "assistant",
		"content": fullResponse.String(),
	})

	return fullResponse.String(), nil
}

func (m *model) moveAgentUp() {
	cursor := m.agentsTable.Cursor()
	log.Printf("Attempting to move agent up. Cursor: %d, Agents Length: %d", cursor, len(m.agents))

	if cursor <= 0 || cursor > len(m.agents) {
		log.Println("Invalid cursor position for moving up.")
		return
	}

	agentIndex := cursor - 1
	log.Printf("Agent Index for moving up: %d", agentIndex)

	if agentIndex > 0 {
		m.agents[agentIndex], m.agents[agentIndex-1] = m.agents[agentIndex-1], m.agents[agentIndex]

		m.populateAgentsTable()

		m.agentsTable.SetCursor(cursor - 1)
		log.Printf("Moved agent up. New cursor position: %d", cursor-1)

		return
	}
}

func (m *model) moveAgentDown() {
	cursor := m.agentsTable.Cursor()
	log.Printf("Attempting to move agent down. Cursor: %d, Agents Length: %d", cursor, len(m.agents))

	if cursor <= 0 || cursor > len(m.agents) {
		log.Println("Invalid cursor position for moving down.")
		return
	}

	agentIndex := cursor - 1
	log.Printf("Agent Index for moving down: %d", agentIndex)

	if agentIndex < len(m.agents)-1 {
		m.agents[agentIndex], m.agents[agentIndex+1] = m.agents[agentIndex+1], m.agents[agentIndex]

		m.populateAgentsTable()

		m.agentsTable.SetCursor(cursor + 1)
		log.Printf("Moved agent down. New cursor position: %d", cursor+1)

		return
	}
}

func saveAgentsCmd(m *model) tea.Cmd {
	return func() tea.Msg {
		err := saveAgents(m)
		if err != nil {
			return errMsg(fmt.Errorf("failed to save agents: %w", err))
		}
		return notifyMsg("Agents reordered and saved successfully.")
	}
}

func (m *model) handleEnterKey() (tea.Model, tea.Cmd) {
	switch m.viewMode {
	case ChatListView:
		selectedItem := m.chatList.SelectedItem()
		if selectedItem == nil {
			return m, nil
		}

		if chatItem, ok := selectedItem.(chatItem); ok {
			if chatItem.chat.Name == "Create New Chat" {
				m.viewMode = NewChatFormView
				m.formActive = true
				m.newChatName = ""
				m.newProjectName = ""
				m.newChatForm = createNewChatForm(&m.newChatName, &m.newProjectName)
				return m, nil
			}

			// Use the new selection handler
			m.handleChatSelection(&chatItem.chat)
			return m, nil
		}

	case NewChatFormView:
		if m.newChatForm.State == huh.StateCompleted {
			if m.newChatName == "" {
				m.errorMessage = "Chat name cannot be empty"
				m.newChatForm.State = huh.StateNormal
				return m, nil
			}
			if m.newProjectName == "" {
				m.errorMessage = "Project name cannot be empty"
				m.newChatForm.State = huh.StateNormal
				return m, nil
			}

			// Create new chat with both name and project name
			err := m.createNewChat(m.newChatName, m.newProjectName)
			if err != nil {
				m.errorMessage = fmt.Sprintf("Failed to create new chat: %v", err)
				return m, nil
			}

			// Reset form and fields for next use
			m.newChatName = ""
			m.newProjectName = ""
			m.newChatForm = createNewChatForm(&m.newChatName, &m.newProjectName)

			// Switch back to ChatView
			m.viewMode = ChatView
			m.formActive = false
			m.updateViewport()
			return m, nil
		}

	case InsertView:
		if !m.formActive && !m.agentFormActive {
			m.currentUserMessage = m.textarea.Value()
			m.textarea.Reset()
			m.loading = true
			m.viewMode = ChatView
			m.textarea.Blur()
			return m, sendChatMessage(m)
		}
	case ModelView:
		selectedRow := m.modelTable.SelectedRow()
		if selectedRow == nil {
			return m, nil
		}
		modelName := selectedRow[0]
		if modelName == "Add New Model" {
			m.viewMode = AvailableModelsView
			m.availableTable.Focus()
			m.modelTable.Blur()
			return m, fetchAvailableModelsCmd()
		}
		m.confirmDeleteModelName = modelName
		m.confirmDeleteType = "model"
		m.confirmForm = createConfirmForm(fmt.Sprintf("Are you sure you want to delete model '%s'? This action cannot be undone.", modelName), &m.confirmResult)
		m.viewMode = ConfirmDelete
		m.modelTable.Blur()
		return m, nil
	case AvailableModelsView:
		selectedRow := m.availableTable.SelectedRow()
		if selectedRow == nil {
			return m, nil
		}
		modelName := selectedRow[0]
		var selectedModel AvailableModel
		for _, mdl := range m.availableModels {
			if mdl.Name == modelName {
				selectedModel = mdl
				break
			}
		}
		if selectedModel.Name == "" {
			return m, nil
		}
		m.selectedAvailableModel = selectedModel
		m.populateParameterSizesTable(selectedModel.Sizes)
		m.viewMode = ParameterSizesView
		m.parameterSizesTable.Focus()
		m.availableTable.Blur()
		return m, nil
	case ParameterSizesView:
		selectedRow := m.parameterSizesTable.SelectedRow()
		if selectedRow == nil {
			return m, nil
		}
		size := selectedRow[0]
		modelName := m.selectedAvailableModel.Name
		fullModelName := modelName
		if size != "" {
			fullModelName = fmt.Sprintf("%s:%s", modelName, size)
		}
		m.viewMode = DownloadingView
		m.parameterSizesTable.Blur()
		return m, tea.Batch(downloadModelCmd(fullModelName), m.spinner.Tick)
	case AgentView:
		selectedRow := m.agentsTable.SelectedRow()
		if selectedRow == nil {
			return m, nil
		}
		agentRole := selectedRow[0]
		if agentRole == "Add New Agent" {
			m.agentAction = "add"
			m.currentEditingAgent = Agent{}
			m.agentForm = createAgentForm(&m.currentEditingAgent, m.availableModelVersions, m.availableTools)
			m.agentFormActive = true
			m.viewMode = AgentFormView
			m.agentsTable.Blur()
			return m, nil
		} else {
			for _, agent := range m.agents {
				if strings.EqualFold(agent.Role, agentRole) {
					m.selectedAgent = agent
					m.currentEditingAgent = agent
					break
				}
			}

			m.agentAction = "edit"
			m.agentForm = createAgentForm(&m.currentEditingAgent, m.availableModelVersions, m.availableTools)
			m.agentFormActive = true
			m.viewMode = AgentFormView
			m.agentsTable.Blur()
			return m, nil
		}
	}
	return m, nil
}

func (m model) View() string {
	if m.errorMessage != "" {
		return fmt.Sprintf(
			"%s\n\nPress 'r' to retry or any other key to continue.",
			errorStyle.Render(m.errorMessage),
		)
	}

	return m.ViewWithoutError()
}

func (m model) ViewWithoutError() string {
	if m.formActive {
		switch m.viewMode {
		case NewChatFormView:
			return m.newChatForm.View()
		case AgentFormView:
			return m.agentForm.View()
		default:
			return m.configForm.View()
		}
	}

	if m.agentFormActive {
		return m.agentForm.View()
	}

	if m.viewMode == ConfirmDelete && m.confirmForm != nil {
		return "Confirmation:\n\n" + m.confirmForm.View()
	}

	switch m.viewMode {
	case ChatListView:
		header := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#666666")).
			Padding(0, 1).
			MarginBottom(1). // Add margin below header
			Render("Chat List (Enter to select, / to search, ESC to go back)")

		return fmt.Sprintf("%s\n%s", header, m.chatList.View())

	case NewChatFormView:
		m.newChatForm = createNewChatForm(&m.newChatName, &m.newProjectName) // Updated function call
		return m.newChatForm.View()

	case ModelView:
		var status string
		if m.ollamaRunning {
			status = "Ollama Serve: Running"
		} else {
			status = "Ollama Serve: Stopped"
		}
		indicator := m.indicatorStyle().Render(status)

		if len(m.modelTable.Rows()) == 0 {
			return indicator + "\nNo models available."
		}

		return indicator + "\n" + m.modelTable.View()

	case AgentView:
		return m.agentView()
	case AgentFormView:
		return m.agentFormView()
	case AvailableModelsView:
		return "Available Ollama Models:\n\n" + m.availableTable.View()
	case ParameterSizesView:
		return fmt.Sprintf("Select Parameter Size for '%s':\n\n%s", m.selectedAvailableModel.Name, m.parameterSizesTable.View())
	case DownloadingView:
		return fmt.Sprintf("%s Downloading model, feel free to exit this page", m.spinner.View())
	case InsertView:
		return m.viewport.View() + "\n" + m.textarea.View()
	default:
		return m.viewport.View() + "\n" + m.textarea.View()
	}
}

func (m model) agentView() string {
	return fmt.Sprintf(
		"Agents (Press 'u' to move up, 'y' to move down):\n\n%s\n\nPress 'a' to Add, 'e' to Edit, 'd' to Delete an agent, 'g' to Go Back.",
		m.agentsTable.View(),
	)
}

func (m model) agentFormView() string {
	return "Configure Agent:\n\n" + m.agentForm.View()
}

func fetchModelsCmd() tea.Cmd {
	return func() tea.Msg {
		models, err := fetchModels()
		if err != nil {
			log.Printf("Failed to fetch models from /api/tags: %v", err)
			return modelsMsg{}
		}
		return modelsMsg(models)
	}
}

func (m *model) populateModelTable(models []OllamaModel) {
	var rows []table.Row

	rows = append(rows, table.Row{"Add New Model", "N/A", "N/A"})

	for _, mdl := range models {
		rows = append(rows, table.Row{
			mdl.Name,
			mdl.Details.ParameterSize,
			FormatSizeGB(mdl.Size),
		})
	}
	m.modelTable.SetRows(rows)
}

func (m *model) populateAvailableModelsTable(models []AvailableModel) {
	var rows []table.Row
	for _, mdl := range models {
		sizes := strings.Join(mdl.Sizes, ", ")
		rows = append(rows, table.Row{
			mdl.Name,
			sizes,
		})
	}
	m.availableTable.SetRows(rows)
}

func (m *model) populateParameterSizesTable(sizes []string) {
	var rows []table.Row
	for _, size := range sizes {
		rows = append(rows, table.Row{size})
	}
	columns := []table.Column{
		{Title: "Available Sizes", Width: 20},
	}
	m.parameterSizesTable.SetColumns(columns)
	m.parameterSizesTable.SetRows(rows)

	if m.viewMode == ParameterSizesView && m.parameterSizesTable.Focused() && len(rows) > 0 {
		m.parameterSizesTable.SetCursor(0)
	}
}

func deleteModelCmd(modelName string) tea.Cmd {
	return func() tea.Msg {
		err := deleteModel(modelName)
		if err != nil {
			return errMsg(err)
		}
		return modelDeletedMsg{}
	}
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

func deleteAgentCmd(agentRole string) tea.Cmd {
	return func() tea.Msg {
		// TODO Perform deletion logic, e.g., API call or local deletion

		// Here, return a message indicating deletion
		return agentDeletedMsg{Role: agentRole}
	}
}

func (m *model) saveCurrentChat() error {
	if m.selectedChat == nil {
		return fmt.Errorf("no chat selected")
	}

	// Update the selected chat's messages with the current conversation history
	m.selectedChat.Messages = m.conversationHistory

	// Save the updated chat to file
	return saveChat(*m.selectedChat, m.chatsFolderPath)
}

func sendChatMessage(m *model) tea.Cmd {
	return func() tea.Msg {
		if m.currentUserMessage == "" {
			log.Println("No user message to send.")
			return nil
		}

		// Add user message to conversation history
		m.conversationHistory = append(m.conversationHistory, map[string]string{
			"role":    "user",
			"content": m.currentUserMessage,
		})

		if len(m.agents) == 0 {
			return errMsg(fmt.Errorf("no agents configured"))
		}

		var lastResponse string
		currentInput := m.currentUserMessage

		// Process each agent in sequence
		for _, agent := range m.agents {
			response, err := processAgentChain(currentInput, m, agent)
			if err != nil {
				return errMsg(fmt.Errorf("error processing agent '%s': %w", agent.Role, err))
			}
			lastResponse = response
			currentInput = response // Use this agent's response as input for the next agent
		}

		// Update the model's state
		m.assistantResponses = append(m.assistantResponses, lastResponse)
		m.userMessages = append(m.userMessages, m.currentUserMessage)
		m.currentUserMessage = ""
		m.loading = false
		m.viewMode = ChatView
		m.textarea.Blur()
		m.updateViewport()

		// Save the updated chat
		if err := m.saveCurrentChat(); err != nil {
			return errMsg(fmt.Errorf("failed to save chat: %w", err))
		}

		return responseMsg("Conversation processed successfully.")
	}
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

	// Clean up the code
	code := toolCall.Parameters.Code
	// Remove literal \n and replace with actual newlines
	code = strings.ReplaceAll(code, "\\n", "\n")
	// Remove escaped quotes
	code = strings.ReplaceAll(code, "\\\"", "\"")
	// Remove any triple quotes that might be present
	code = strings.Trim(code, "\"\"\"")

	return code, nil
}

func escapeJSONString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return s
	}
	// Remove the surrounding quotes that json.Marshal adds
	return string(b[1 : len(b)-1])
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

	// Create a temporary project structure
	tmpDir, err := ioutil.TempDir("", "golint_*")
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
	if err := ioutil.WriteFile(codeFile, []byte(formattedCode), 0644); err != nil {
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

// Helper function to copy a file
func copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

func processAgentsSequentially(m *model, input string, agents []Agent) ([]string, error) {
	var responses []string
	currentInput := input

	for _, agent := range agents {
		response, err := processAgentChain(currentInput, m, agent)
		if err != nil {
			log.Printf("Error processing agent '%s': %v\n", agent.Role, err)
			return responses, err
		}
		responses = append(responses, response)
		currentInput = response
	}

	return responses, nil
}

func processAgent(messages []map[string]string, m *model, agent Agent) (string, error) {
	// First, check if this agent has the code checking tool
	hasCodeChecker := false
	for _, tool := range agent.Tools {
		if tool.Name == "check_go_code" {
			hasCodeChecker = true
			break
		}
	}

	// Extract code from the last message (either user or another agent)
	var codeToCheck string
	var lastMessage string
	if len(messages) > 0 {
		lastMessage = messages[len(messages)-1]["content"]
		// Extract any code blocks from the message
		codeBlocks := extractCodeBlocks(lastMessage)
		if len(codeBlocks) > 0 {
			codeToCheck = strings.Join(codeBlocks, "\n")
		}
	}

	// If the agent has the code checker and there's code to check, enhance the prompt
	if hasCodeChecker && codeToCheck != "" {
		contextContent, err := loadFileContext(agent.ContextFilePath)
		if err != nil {
			return "", fmt.Errorf("failed to load context for agent '%s': %w", agent.Role, err)
		}

		// Create an enhanced system prompt that includes code checking instructions
		enhancedSystemPrompt := fmt.Sprintf(`%s

You have access to a Go code checking tool. When you receive code, you should:
2. Use the check_go_code tool to verify it
3. Review the tool's output
4. Provide your analysis and suggestions based on both your review and the tool's output
5. If there are issues, point them out

Context: %s`, agent.SystemPrompt, contextContent)

		// Prepare the API request with tools
		payload := map[string]interface{}{
			"model": agent.ModelVersion,
			"messages": []map[string]string{
				{
					"role":    "system",
					"content": enhancedSystemPrompt,
				},
				{
					"role":    "user",
					"content": lastMessage,
				},
			},
			"stream": false,
			"tools": []map[string]interface{}{
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
			},
		}

		requestBody, err := json.Marshal(payload)
		if err != nil {
			return "", fmt.Errorf("failed to marshal request body: %w", err)
		}

		// Send request to Ollama API
		resp, err := http.Post(ollamaAPIURL+"/chat", "application/json", bytes.NewBuffer(requestBody))
		if err != nil {
			return "", fmt.Errorf("failed to send request to Ollama API: %w", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := ioutil.ReadAll(resp.Body)
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

		// Handle tool calls and build response
		var fullResponse strings.Builder
		fullResponse.WriteString(apiResponse.Message.Content)

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
					fullResponse.WriteString("\n\nLint Check Result:\n" + lintResult)
				} else {
					fullResponse.WriteString("\n\nLint Check Result:\n" + lintResult)
				}
			}
		}

		return fullResponse.String(), nil
	}

	// If no code checker or no code to check, process normally
	return dynamicAgentBehavior(messages, m, agent)
}

func dynamicAgentBehavior(messages []map[string]string, m *model, agent Agent) (string, error) {
	contextContent, err := loadFileContext(agent.ContextFilePath)
	if err != nil {
		return "", fmt.Errorf("failed to load context for agent '%s': %w", agent.Role, err)
	}

	systemPrompt := strings.ReplaceAll(agent.SystemPrompt, "{context}", contextContent)

	systemMessage := map[string]string{
		"role":    "system",
		"content": systemPrompt,
	}
	messagesWithSystem := append([]map[string]string{systemMessage}, messages...)

	numCtx, err := strconv.Atoi(agent.Tokens)
	if err != nil || numCtx <= 0 {
		numCtx = 16384
	}

	options := map[string]interface{}{
		"num_ctx": numCtx,
	}

	requestBody, err := json.Marshal(map[string]interface{}{
		"model":    agent.ModelVersion,
		"messages": messagesWithSystem,
		"stream":   false,
		"options":  options,
	})
	if err != nil {
		return "", fmt.Errorf("failed to marshal request body for agent '%s': %w", agent.Role, err)
	}

	req, err := http.NewRequest("POST", ollamaAPIURL+"/chat", bytes.NewBuffer(requestBody))
	if err != nil {
		return "", fmt.Errorf("failed to create HTTP request for agent '%s': %w", agent.Role, err)
	}
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed for agent '%s': %w", agent.Role, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("error from Ollama API for agent '%s': %v", agent.Role, resp.Status)
	}

	var rawResponse map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&rawResponse); err != nil {
		return "", fmt.Errorf("failed to decode response for agent '%s': %w", agent.Role, err)
	}

	if message, ok := rawResponse["message"].(map[string]interface{}); ok {
		if content, ok := message["content"].(string); ok {
			return content, nil
		}
	}

	return "", fmt.Errorf("unexpected response format or empty response for agent '%s': %+v", agent.Role, rawResponse)
}

func messagesForAgent(m *model, agent Agent) []map[string]string {
	messages := make([]map[string]string, len(m.conversationHistory))
	copy(messages, m.conversationHistory)

	systemMessage := map[string]string{
		"role":    "system",
		"content": agent.SystemPrompt,
	}

	messages = append([]map[string]string{systemMessage}, messages...)

	return messages
}

func (m *model) updateViewport() {
	var conversation strings.Builder
	for _, msg := range m.conversationHistory {
		role := strings.Title(msg["role"])
		content := msg["content"]

		switch strings.ToLower(role) {
		case "user":
			conversation.WriteString(fmt.Sprintf("**%s:**\n\n%s\n\n", role, content))
		case "assistant":
			conversation.WriteString(fmt.Sprintf("**%s:**\n\n%s\n\n", role, content))
		case "tool":
			conversation.WriteString(fmt.Sprintf("**%s:**\n\n```plaintext\n%s\n```\n\n", role, content))
		default:
			conversation.WriteString(fmt.Sprintf("**%s:**\n\n%s\n\n", role, content))
		}
	}

	renderedContent, err := m.renderer.Render(conversation.String())
	if err != nil {
		log.Printf("Error rendering conversation: %v", err)
		return
	}
	m.viewport.SetContent(renderedContent)
	m.viewport.GotoBottom()
	m.viewport.Height = m.height - 3
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

func FormatSizeGB(size int64) string {
	gb := float64(size) / (1024 * 1024 * 1024)
	return fmt.Sprintf("%.1f GB", gb)
}

func retrieveRelevantSections(query, filePath string) (string, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	var relevantSections strings.Builder
	lines := strings.Split(string(content), "\n")
	for _, line := range lines {
		if strings.Contains(line, query) {
			relevantSections.WriteString(line + "\n")
		}
	}

	return relevantSections.String(), nil
}

func generateCode(messages []map[string]string, m *model, agent Agent) (string, error) {
	contextBytes, err := os.ReadFile(agent.ContextFilePath)
	if err != nil {
		return "", fmt.Errorf("failed to read context file: %w", err)
	}
	context := string(contextBytes)

	systemPrompt := strings.ReplaceAll(agent.SystemPrompt, "{context}", context)

	systemMessage := map[string]string{
		"role":    "system",
		"content": systemPrompt,
	}
	messagesWithSystem := append([]map[string]string{systemMessage}, messages...)

	response, err := requestOllama(messagesWithSystem, agent)
	if err != nil {
		return "", err
	}

	if strings.Contains(response, "func ") || strings.Contains(response, "package ") {
		formattedResponse := fmt.Sprintf("```go\n%s\n```", response)
		return formattedResponse, nil
	}

	return response, nil
}

func testCode(messages []map[string]string, m *model, agent Agent) (string, error) {
	assistantCode := ""
	for i := len(messages) - 1; i >= 0; i-- {
		if strings.ToLower(messages[i]["role"]) == "assistant" {
			assistantCode = messages[i]["content"]
			break
		}
	}

	if assistantCode == "" {
		return "No code for agent to test.", nil
	}

	codeBlocks := extractCodeBlocks(assistantCode)
	if len(codeBlocks) == 0 {
		return "No code for agent to test.", nil
	}

	codeToTest := strings.Join(codeBlocks, "\n")

	systemPrompt := "You are a code tester tasked with reviewing the following code for potential bugs or issues. Identify and highlight any issues or improvements needed."

	messagesForTester := []map[string]string{
		{
			"role":    "system",
			"content": systemPrompt,
		},
		{
			"role":    "user",
			"content": codeToTest,
		},
	}

	return requestOllama(messagesForTester, agent)
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
				// Starting a code block
				inCodeBlock = true
				isGoBlock = strings.HasPrefix(line, "```go")
				currentBlock.Reset()
			} else {
				// Ending a code block
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

func fetchAvailableModelsCmd() tea.Cmd {
	return func() tea.Msg {
		models, err := scrapeOllamaLibrary()
		if err != nil {
			return errMsg(err)
		}
		return availableModelsMsg(models)
	}
}

// function to scrape ollama library
func scrapeOllamaLibrary() ([]AvailableModel, error) {
	url := "https://ollama.com/library"
	response, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve the page: %v", err)
	}
	defer response.Body.Close()

	if response.StatusCode != 200 {
		return nil, fmt.Errorf("failed to retrieve the page. Status code: %d", response.StatusCode)
	}

	doc, err := goquery.NewDocumentFromReader(response.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %v", err)
	}

	models := parseContent(doc)

	if len(models) == 0 {
		return nil, fmt.Errorf("no models found in the library")
	}

	return models, nil
}

// helper function for scrapeOllamaLibrary
func parseContent(doc *goquery.Document) []AvailableModel {
	var models []AvailableModel
	liElements := doc.Find("li")

	liElements.Each(func(i int, li *goquery.Selection) {
		if !li.HasClass("flex") || !li.HasClass("items-baseline") {
			return
		}

		var model AvailableModel

		nameElem := li.Find("h2")
		if nameElem.Length() > 0 {
			nameSpan := nameElem.Find("span")
			if nameSpan.Length() > 0 {
				model.Name = strings.TrimSpace(nameSpan.Text())
			}
		}

		sizes := []string{}
		sizeElements := li.Find("span")
		sizeElements.Each(func(i int, span *goquery.Selection) {
			if span.HasClass("inline-flex") && span.HasClass("items-center") &&
				span.HasClass("rounded-md") && span.HasClass("bg-[#ddf4ff]") {
				sizes = append(sizes, strings.TrimSpace(span.Text()))
			}
		})
		if len(sizes) > 0 {
			model.Sizes = sizes
		}

		if model.Name != "" {
			models = append(models, model)
		}
	})

	return models
}

type AvailableModel struct {
	Name  string   `json:"name"`
	Sizes []string `json:"sizes"`
}

func downloadModelCmd(modelName string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("ollama", "pull", modelName)
		output, err := cmd.CombinedOutput()
		if err != nil {
			return errMsg(fmt.Errorf("failed to download model: %v, output: %s", err, string(output)))
		}
		return modelDownloadedMsg(modelName)
	}
}

func ValidateAgentRole(m *model, newRole string, originalRole string) error {
	for _, agent := range m.agents {
		if strings.EqualFold(agent.Role, newRole) && !strings.EqualFold(agent.Role, originalRole) {
			return fmt.Errorf("agent role '%s' already exists", newRole)
		}
	}
	return nil
}

func keyIsCtrlZ(msg tea.KeyMsg) bool {
	return msg.Type == tea.KeyCtrlZ
}

func main() {
	model := InitialModel()
	p := tea.NewProgram(model)
	if _, err := p.Run(); err != nil {
		os.Exit(1)
	}
}
