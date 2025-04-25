package main

import (
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/filepicker"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

func (m *model) Init() tea.Cmd {
	return tea.Batch(
		textarea.Blink,
		tea.EnterAltScreen,
		fetchModelsCmd(),
		m.spinner.Tick,
		func() tea.Msg {
			return initialTransitionMsg{}
		},
	)
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

	fp := filepicker.New()
	fp.CurrentDirectory, _ = os.Getwd()
	fp.AllowedTypes = []string{".jpg", ".jpeg", ".png", ".gif", ".webp"}
	fp.Height = 10

	modelColumns := []table.Column{
		{Title: "Name", Width: 30},
		{Title: "Parameter Size", Width: 15},
		{Title: "Size (GB)", Width: 10},
	}

	modelTable := table.New(
		table.WithColumns(modelColumns),
		table.WithFocused(false),
		table.WithStyles(tableStyle),
	)
	modelTable.SetRows([]table.Row{
		{"Add New Model", "N/A", "N/A"},
	})

	availableColumns := []table.Column{
		{Title: "Available Models", Width: 30},
		{Title: "Sizes", Width: 20},
	}

	availableTable := table.New(
		table.WithColumns(availableColumns),
		table.WithFocused(false),
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
		{Title: "Role", Width: 20},
		{Title: "Model Version", Width: 40},
	}

	agentsTable := table.New(
		table.WithColumns(agentColumns),
		table.WithFocused(false),
		table.WithStyles(tableStyle),
	)

	availableTools := []Tool{
		checkGoCodeTool,
	}

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
		availableTools:         availableTools,
		availableModelVersions: []string{},
		modelsFetchError:       nil,
		errorMessage:           "",
		confirmDeleteType:      "",
		toolUsages:             []ToolUsage{},
		toolUsageFilePath:      "./tool_usages.json",
		filePicker:             fp,
		selectedImage:          "",
	}

	err := loadAgents(m)
	if err != nil {
		log.Printf("Error loading agents from file: %v", err)
		m.agents = append(m.agents, Agent{
			Role:            "Assistant",
			ModelVersion:    "",
			SystemPrompt:    "",
			UseContext:      false,
			ContextFilePath: "",
			UseConversation: false,
			Tokens:          "2048",
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

	m.agentForm = createAgentForm(&m.currentEditingAgent, m.availableModelVersions, m.availableTools)

	m.availableModelVersions = []string{defaultModelVersion}

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

	if m.viewMode == ChatListView {
		return m.updateChatList(msg)
	}

	if m.errorMessage != "" {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "esc", "q":
				m.errorMessage = ""
				return m, nil
			case "r":
				m.errorMessage = ""
				return m, fetchModelsCmd()
			}
		default:
			return m, nil
		}
	}

	// global key handling (esc/ctrl+z)
	switch msg := msg.(type) {
	case initialTransitionMsg:
		m.viewMode = ChatListView
		return m, triggerWindowResize(m.width, m.height)

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
					return m, fetchModelsCmd()
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

				err := m.createNewChat(m.newChatName, m.newProjectName)
				if err != nil {
					m.errorMessage = fmt.Sprintf("Failed to create new chat: %v", err)
					return m, nil
				}

				m.newChatName = ""
				m.newProjectName = ""
				m.newChatForm = createNewChatForm(&m.newChatName, &m.newProjectName)

				m.viewMode = ChatView
				m.formActive = false
				m.updateViewport()
				return m, nil
			}
		}
		return m, formCmd
	}

	if m.agentFormActive {
		updatedForm, formCmd := m.agentForm.Update(msg)
		m.agentForm = updatedForm.(*huh.Form)

		switch m.agentForm.State {
		case huh.StateCompleted:
			m.currentEditingAgent.Tools = []Tool{}

			for _, toolName := range m.currentEditingAgent.SelectedTools {
				for _, availableTool := range m.availableTools {
					if availableTool.Name == toolName {
						m.currentEditingAgent.Tools = append(m.currentEditingAgent.Tools, availableTool)
						break
					}
				}
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

			err := saveAgents(m)
			if err != nil {
				m.errorMessage = fmt.Sprintf("Failed to save agents: %v", err)
				return m, nil
			}

			return m, nil
		}

		return m, formCmd
	}

	if m.viewMode == ConfirmDelete && m.confirmForm != nil {
		updatedConfirmForm, confirmCmd := m.confirmForm.Update(msg)
		m.confirmForm = updatedConfirmForm.(*huh.Form)

		switch m.confirmForm.State {
		case huh.StateCompleted:
			if m.confirmDeleteType == "model" {
				m.viewMode = ModelView
				if m.confirmResult {
					return m, tea.Sequence(
						deleteModelCmd(m.confirmDeleteModelName),
						func() tea.Msg {
							m.confirmDeleteModelName = ""
							m.agentToDelete = ""
							m.confirmDeleteType = ""
							m.confirmForm = nil
							m.modelTable.Focus()
							return nil
						},
						func() tea.Msg {
							models, err := fetchModels()
							if err != nil {
								return errMsg(err)
							}
							m.populateModelTable(models)

							m.viewMode = ModelView
							m.modelTable.Focus()
							m.textarea.Blur()
							m.availableTable.Blur()
							m.agentsTable.Blur()
							m.parameterSizesTable.Blur()

							return modelsMsg(models)
						},
					)
				} else {
					return m, tea.Sequence(
						func() tea.Msg {
							m.confirmDeleteModelName = ""
							m.agentToDelete = ""
							m.confirmDeleteType = ""
							m.confirmForm = nil
							m.modelTable.Focus()
							return nil
						},
						func() tea.Msg {
							models, err := fetchModels()
							if err != nil {
								return errMsg(err)
							}
							m.populateModelTable(models)
							return modelsMsg(models)
						},
					)
				}
			} else if m.confirmDeleteType == "agent" {
				m.viewMode = AgentView
				if m.confirmResult {
					return m, tea.Sequence(
						deleteAgentCmd(m.agentToDelete),
						func() tea.Msg {
							m.confirmDeleteModelName = ""
							m.agentToDelete = ""
							m.confirmDeleteType = ""
							m.confirmForm = nil
							m.agentsTable.Focus()
							return nil
						},
					)
				} else {
					m.confirmDeleteModelName = ""
					m.agentToDelete = ""
					m.confirmDeleteType = ""
					m.confirmForm = nil
					m.agentsTable.Focus()
					return m, nil
				}
			}

			m.confirmDeleteModelName = ""
			m.agentToDelete = ""
			m.confirmDeleteType = ""
			m.confirmForm = nil
			return m, nil
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
		case "f":
			if m.viewMode == ChatView || m.viewMode == InsertView {
				m.viewMode = FilePickerView
				return m, nil
			}
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
				return m, fetchModelsCmd()
			}
		case "l":
			if m.viewMode == ChatView {
				m.viewMode = ChatListView
				return m, triggerWindowResize(m.width, m.height)
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
			if m.viewMode == FilePickerView {
				m.viewMode = ChatView
				return m, nil
			}
			switch m.viewMode {
			case AgentFormView:
				m.viewMode = AgentView
				m.agentFormActive = false
				m.agentsTable.Focus()
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

		if m.viewMode == ChatListView {
			headerHeight := 2
			m.chatList.SetSize(msg.Width-2, msg.Height-headerHeight)
		}

		// WIP: file picker for mm inputs
		if m.viewMode == FilePickerView {
			var fpCmd tea.Cmd
			m.filePicker, fpCmd = m.filePicker.Update(msg)

			if didSelect, path := m.filePicker.DidSelectFile(msg); didSelect {
				base64Image, err := m.loadImageAsBase64(path)
				if err != nil {
					m.errorMessage = fmt.Sprintf("Failed to load image: %v", err)
				} else {
					m.conversationHistory = append(m.conversationHistory, map[string]string{
						"role":    "user",
						"content": fmt.Sprintf("![Selected Image](%s)", base64Image),
					})
					m.selectedImage = path
					m.updateViewport()
				}
				m.viewMode = ChatView
				return m, nil
			}

			return m, fpCmd
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

	if m.viewMode == ChatListView {
		return m.updateChatList(msg)
	}

	return m, cmd
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

			err := m.createNewChat(m.newChatName, m.newProjectName)
			if err != nil {
				m.errorMessage = fmt.Sprintf("Failed to create new chat: %v", err)
				return m, nil
			}

			m.newChatName = ""
			m.newProjectName = ""
			m.newChatForm = createNewChatForm(&m.newChatName, &m.newProjectName)

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
	case FilePickerView:
		return fmt.Sprintf(
			"Select an image file:\n\n%s\n\n(press esc to cancel)",
			m.filePicker.View(),
		)
	case ChatListView:
		header := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#666666")).
			Padding(0, 1).
			MarginBottom(1).
			Render("Chat List (Enter to select, / to search, ESC to go back)")

		return fmt.Sprintf("%s\n%s", header, m.chatList.View())

	case NewChatFormView:
		m.newChatForm = createNewChatForm(&m.newChatName, &m.newProjectName)
		return m.newChatForm.View()

	case ModelView:
		var status string
		if m.ollamaRunning {
			status = "Ollama Serve: Running"
		} else {
			status = "Ollama Serve: Stopped"
		}
		indicator := m.indicatorStyle().Render(status)

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

func triggerWindowResize(width, height int) tea.Cmd {
	return func() tea.Msg {
		return tea.WindowSizeMsg{
			Width:  width,
			Height: height,
		}
	}
}

func (m *model) updateViewport() {
	var conversation strings.Builder
	titleCaser := cases.Title(language.English)

	for _, msg := range m.conversationHistory {
		role := titleCaser.String(msg["role"])
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

func main() {
	model := InitialModel()
	p := tea.NewProgram(model)
	if _, err := p.Run(); err != nil {
		os.Exit(1)
	}
}
