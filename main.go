package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

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

type Agent struct {
	Role            string `json:"role"`
	ModelVersion    string `json:"model_version"`
	SystemPrompt    string `json:"system_prompt"`
	ContextFilePath string `json:"context_file_path"`
	Tokens          string `json:"tokens"`
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
)

const (
	defaultSystemPrompt     = "You are an assistant tasked with generating code based on the user's prompt. Use the following context to generate the best solution. Context: {context}"
	defaultContextFilePath  = "/home/cvberscape/code/agentui/main.go"
	defaultTokens           = "16384"
	defaultModelVersion     = "llama3.1"
	ollamaAPIURL            = "http://localhost:11434/api"
	defaultIndicatorPrompt  = "│"
	configFormTitle         = "Chat Configuration"
	agentFormTitle          = "Agent Configuration"
	confirmDeleteAgentTitle = "Confirm Agent Deletion"
	confirmDeleteModelTitle = "Confirm Model Deletion"
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
	testerResponses        []string
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

func createAgentForm(agent *Agent, modelVersions []string) *huh.Form {
	modelOptions := make([]huh.Option[string], 0, len(modelVersions))
	for _, mv := range modelVersions {
		modelOptions = append(modelOptions, huh.NewOption(mv, mv))
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

			huh.NewInput().
				Title("Context File Path").
				Placeholder("/path/to/context/file").
				Value(&agent.ContextFilePath),

			huh.NewInput().
				Title("Tokens").
				Placeholder("16384").
				Value(&agent.Tokens),
		),
	).WithShowHelp(true)
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
	vp := viewport.New(50, 20)
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

	m := &model{
		userMessages:        make([]string, 0),
		assistantResponses:  make([]string, 0),
		testerResponses:     make([]string, 0),
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
		formActive:      false,
		agents:          []Agent{},
		agentsTable:     agentsTable,
		agentViewMode:   ChatView,
		agentFormActive: false,

		availableModelVersions: []string{},
		modelsFetchError:       nil,
		errorMessage:           "",
		confirmDeleteType:      "",
	}

	m.agents = append(m.agents, Agent{
		Role:            "Assistant",
		ModelVersion:    "llama3.1",
		SystemPrompt:    "You are an assistant tasked with generating code based on the user's prompt.",
		ContextFilePath: "/path/to/context/file",
		Tokens:          "16384",
	}, Agent{
		Role:            "Tester",
		ModelVersion:    "llama3.1",
		SystemPrompt:    "You are a code tester tasked with reviewing the following code for potential bugs or issues.",
		ContextFilePath: "/path/to/context/file",
		Tokens:          "16384",
	})

	m.populateAgentsTable()

	m.availableModelVersions = []string{defaultModelVersion}

	m.configForm = createConfigForm(&m.config, m.availableModelVersions)

	m.updateTextareaIndicatorColor()

	return m
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

	m.agentsTable.SetCursor(0)
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

	if m.formActive {
		updatedForm, formCmd := m.configForm.Update(msg)
		m.configForm = updatedForm.(*huh.Form)

		switch m.configForm.State {
		case huh.StateCompleted:
			m.formActive = false
			m.viewMode = ChatView
			m.textarea.Focus()
			m.updateViewport()
			return m, nil
		}

		return m, formCmd
	}

	if m.agentFormActive {
		updatedForm, formCmd := m.agentForm.Update(msg)
		m.agentForm = updatedForm.(*huh.Form)

		switch m.agentForm.State {
		case huh.StateCompleted:
			originalRole := ""
			if m.agentAction == "edit" {
				originalRole = m.selectedAgent.Role
			}

			if err := ValidateAgentRole(m, m.currentEditingAgent.Role, originalRole); err != nil {
				m.errorMessage = err.Error()
				return m, nil
			}

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
			return m, nil
		}

		return m, formCmd
	}

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
		case "a":
			if m.viewMode == AgentView {
				m.agentAction = "add"
				m.currentEditingAgent = Agent{}
				m.agentForm = createAgentForm(&m.currentEditingAgent, m.availableModelVersions)
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
				m.agentForm = createAgentForm(&m.currentEditingAgent, m.availableModelVersions)
				m.agentFormActive = true
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

		switch m.viewMode {
		case AgentView:
			availableHeight := m.height - 4
			if availableHeight < 3 {
				availableHeight = 3
			}
			m.agentsTable.SetHeight(availableHeight)
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
		log.Printf("Ollama Serve toggled. Now running: %v", m.ollamaRunning)
		return m, nil
	}

	return m, cmd
}

func (m *model) handleEnterKey() (tea.Model, tea.Cmd) {
	switch m.viewMode {
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
			m.agentForm = createAgentForm(&m.currentEditingAgent, m.availableModelVersions)
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
			m.agentForm = createAgentForm(&m.currentEditingAgent, m.availableModelVersions)
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
		return m.configForm.View()
	}

	if m.agentFormActive {
		return m.agentForm.View()
	}

	if m.viewMode == ConfirmDelete && m.confirmForm != nil {
		return "Confirmation:\n\n" + m.confirmForm.View()
	}

	switch m.viewMode {
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
	return "Agents:\n\n" + m.agentsTable.View() + "\n\nPress 'a' to Add, 'e' to Edit, 'd' to Delete an agent, 'g' to Go Back."
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

func sendChatMessage(m *model) tea.Cmd {
	return func() tea.Msg {
		if m.currentUserMessage == "" {
			return nil
		}

		m.conversationHistory = append(m.conversationHistory, map[string]string{
			"role":    "user",
			"content": m.currentUserMessage,
		})

		for _, agent := range m.agents {
			response, err := processAgent(messagesForAgent(m, agent), m, agent)
			if err != nil {
				return errMsg(err)
			}
			m.conversationHistory = append(m.conversationHistory, map[string]string{
				"role":    agent.Role,
				"content": response,
			})
			m.assistantResponses = append(m.assistantResponses, response)
		}

		m.userMessages = append(m.userMessages, m.currentUserMessage)
		m.currentUserMessage = ""
		m.updateViewport()

		return responseMsg("Conversation processed with configured agents.")
	}
}

func processAgent(messages []map[string]string, m *model, agent Agent) (string, error) {
	switch strings.ToLower(agent.Role) {
	case "assistant":
		return generateCode(messages, m, agent)
	case "tester":
		return testCode(messages, m, agent)
	default:
		return fmt.Sprintf("Agent with role '%s' has no defined behavior.", agent.Role), nil
	}
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
		conversation.WriteString(fmt.Sprintf("**%s:**\n\n%s\n\n", strings.Title(msg["role"]), msg["content"]))
	}

	renderedContent, err := m.renderer.Render(conversation.String())
	if err != nil {
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

	return requestOllama(messagesWithSystem, agent)
}

func testCode(messages []map[string]string, m *model, agent Agent) (string, error) {
	assistantCode := ""
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i]["role"] == "assistant" {
			assistantCode = messages[i]["content"]
			break
		}
	}

	if assistantCode == "" {
		return "No code for agent to test", nil
	}

	codeBlocks := extractCodeBlocks(assistantCode)
	if len(codeBlocks) == 0 {
		return "No code for agent to test", nil
	}

	codeToTest := strings.Join(codeBlocks, "\n")

	systemPrompt := "You are a code tester tasked with reviewing the following code for potential bugs or issues. Identify and highlight any issues or improvements needed."

	systemMessage := map[string]string{
		"role":    "system",
		"content": systemPrompt,
	}

	messagesForTester := []map[string]string{
		systemMessage,
		{"role": "user", "content": codeToTest},
	}

	return requestOllama(messagesForTester, agent)
}

func extractCodeBlocks(input string) []string {
	var codeBlocks []string
	lines := strings.Split(input, "\n")
	var isInCodeBlock bool
	var currentBlock strings.Builder

	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)
		if strings.HasPrefix(trimmedLine, "```") {
			if isInCodeBlock {
				codeBlocks = append(codeBlocks, currentBlock.String())
				currentBlock.Reset()
				isInCodeBlock = false
			} else {
				isInCodeBlock = true
			}
		} else if isInCodeBlock {
			currentBlock.WriteString(line + "\n")
		}
	}

	return codeBlocks
}

type AvailableModel struct {
	Name  string   `json:"name"`
	Sizes []string `json:"sizes"`
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

func scrapeOllamaLibrary() ([]AvailableModel, error) {
	inputFilePath := "./code/ollama_models_html.txt"
	outputFilePath := "./code/ollama_models.json"

	os.MkdirAll("./code/", os.ModePerm)

	var content []byte
	if _, err := os.Stat(inputFilePath); err == nil {
		content, err = os.ReadFile(inputFilePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read input file: %v", err)
		}
	} else {
		url := "https://ollama.com/library"
		response, err := http.Get(url)
		if err != nil {
			return nil, fmt.Errorf("failed to retrieve the page: %v", err)
		}
		defer response.Body.Close()

		if response.StatusCode != 200 {
			return nil, fmt.Errorf("failed to retrieve the page. Status code: %d", response.StatusCode)
		}

		content, err = io.ReadAll(response.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response body: %v", err)
		}

		err = os.WriteFile(inputFilePath, content, 0644)
		if err != nil {
			return nil, fmt.Errorf("failed to write to input file: %v", err)
		}
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(content))
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %v", err)
	}

	models := parseContent(doc)

	outputData, err := json.MarshalIndent(models, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal JSON: %v", err)
	}

	err = os.WriteFile(outputFilePath, outputData, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to write to output file: %v", err)
	}

	return models, nil
}

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
			if span.HasClass("inline-flex") && span.HasClass("items-center") && span.HasClass("rounded-md") && span.HasClass("bg-[#ddf4ff]") {
				sizes = append(sizes, strings.TrimSpace(span.Text()))
			}
		})
		if len(sizes) > 0 {
			model.Sizes = sizes
		}

		models = append(models, model)
	})

	return models
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
