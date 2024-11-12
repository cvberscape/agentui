package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
)

type viewMode int

const (
	ChatView viewMode = iota
	InsertView
	ModelView
	ConfirmDeleteView
	AvailableModelsView
	ParameterSizesView
	DownloadingView
)

var (
	runningIndicatorColor = lipgloss.Color("#00FF00")
	stoppedIndicatorColor = lipgloss.Color("#FF0000")
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
	availableModels        []AvailableModel
	selectedAvailableModel AvailableModel
	spinner                spinner.Model
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

func createConfigForm(config *ChatConfig) *huh.Form {
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Model Version").
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

func createConfirmForm(confirmResult *bool) *huh.Form {
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Confirm Deletion").
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
		Render("│")

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

	columns := []table.Column{
		{Title: "Name", Width: 30},
		{Title: "Parameter Size", Width: 15},
		{Title: "Size (GB)", Width: 10},
	}

	modelTable := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
	)

	availableColumns := []table.Column{
		{Title: "Available Models", Width: 30},
		{Title: "Sizes", Width: 20},
	}

	availableTable := table.New(
		table.WithColumns(availableColumns),
		table.WithFocused(true),
	)

	defaultContextFilePath := "/path/to/your/context.txt"
	defaultSystemPrompt := "You are an assistant tasked with generating code based on the user's prompt. Use the following context to generate the best solution. Context: {context}"
	defaultTokens := "16384"

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
		spinner:             sp,
		renderer:            renderer,
		viewMode:            ChatView,
		ollamaRunning:       false,
		config: ChatConfig{
			ModelVersion:    "llama3.1",
			SystemPrompt:    defaultSystemPrompt,
			ContextFilePath: defaultContextFilePath,
			Tokens:          defaultTokens,
		},
		formActive: false,
	}

	m.configForm = createConfigForm(&m.config)

	m.updateTextareaIndicatorColor()
	return m
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, tea.EnterAltScreen, fetchModelsCmd(), m.spinner.Tick)
}

func (m *model) toggleOllamaServe() tea.Cmd {
	return func() tea.Msg {
		if m.ollamaRunning {
			exec.Command("pkill", "-f", "ollama serve").Run()
		} else {
			exec.Command("ollama", "serve").Start()
		}
		m.ollamaRunning = !m.ollamaRunning
		m.updateTextareaIndicatorColor()
		return tea.WindowSizeMsg{Width: m.width, Height: m.height}
	}
}

func (m *model) updateTextareaIndicatorColor() {
	if m.ollamaRunning {
		m.textarea.Prompt = lipgloss.NewStyle().
			Foreground(runningIndicatorColor).
			Render("│")
	} else {
		m.textarea.Prompt = lipgloss.NewStyle().
			Foreground(stoppedIndicatorColor).
			Render("│")
	}
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+z":
			return m, tea.Quit
		case "o":
			if m.viewMode != InsertView {
				return m, m.toggleOllamaServe()
			}
		case "m":
			if m.viewMode == ChatView {
				m.viewMode = ModelView
				return m, fetchModelsCmd()
			}
		case "i":
			if m.viewMode == ChatView {
				m.viewMode = InsertView
				m.textarea.Focus()
				return m, nil
			}
		case "ctrl+f":
			if m.viewMode == ChatView {
				m.formActive = true
				m.viewMode = InsertView
				m.textarea.Blur()
				m.configForm = createConfigForm(&m.config)
				return m, m.configForm.Init()
			}
		case "esc":
			if m.viewMode == ConfirmDeleteView || m.viewMode == AvailableModelsView || m.viewMode == ParameterSizesView || m.viewMode == DownloadingView {
				m.viewMode = ModelView
				m.confirmDeleteModelName = ""
				return m, fetchModelsCmd()
			}
			m.viewMode = ChatView
			m.formActive = false
			m.textarea.Focus()
			return m, nil
		case "enter":
			if m.viewMode == InsertView && !m.formActive {
				m.currentUserMessage = m.textarea.Value()
				m.textarea.Reset()
				m.loading = true
				m.viewMode = ChatView
				m.textarea.Blur()
				return m, sendChatMessage(m)
			} else if m.viewMode == ModelView {
				selectedRow := m.modelTable.SelectedRow()
				if selectedRow == nil {
					return m, nil
				}
				modelName := selectedRow[0]
				if modelName == "Add New Model" {
					m.viewMode = AvailableModelsView
					return m, fetchAvailableModelsCmd()
				}
			} else if m.viewMode == AvailableModelsView {
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
				return m, nil
			} else if m.viewMode == ParameterSizesView {
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
				return m, tea.Batch(downloadModelCmd(fullModelName), m.spinner.Tick)
			}
		case "j":
			switch m.viewMode {
			case ModelView:
				m.modelTable.MoveDown(1)
			case AvailableModelsView:
				m.availableTable.MoveDown(1)
			case ParameterSizesView:
				m.parameterSizesTable.MoveDown(1)
			case ChatView:
				m.viewport.LineDown(1)
			}
		case "k":
			switch m.viewMode {
			case ModelView:
				m.modelTable.MoveUp(1)
			case AvailableModelsView:
				m.availableTable.MoveUp(1)
			case ParameterSizesView:
				m.parameterSizesTable.MoveUp(1)
			case ChatView:
				m.viewport.LineUp(1)
			}
		case "d":
			if m.viewMode == ModelView {
				selectedRow := m.modelTable.SelectedRow()
				if selectedRow == nil {
					return m, nil
				}
				modelName := selectedRow[0]
				if modelName == "Add New Model" {
					return m, nil
				}
				m.confirmDeleteModelName = modelName
				m.viewMode = ConfirmDeleteView
				m.confirmResult = false
				m.confirmForm = createConfirmForm(&m.confirmResult)
				return m, m.confirmForm.Init()
			}
		}
	case modelsMsg:
		m.populateModelTable(msg)
	case availableModelsMsg:
		m.availableModels = msg
		m.populateAvailableModelsTable(msg)
	case modelDeletedMsg:
		return m, fetchModelsCmd()
	case modelDownloadedMsg:
		m.viewMode = ModelView
		return m, fetchModelsCmd()
	case errMsg:
		m.loading = false
		m.err = msg
		m.modelTable.SetRows(nil)
		return m, nil
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.textarea.SetWidth(m.width)
		m.viewport.Width = m.width
		m.viewport.Height = m.height - 3
		m.updateViewport()

		m.availableTable.SetWidth(m.width)
		m.availableTable.SetHeight(m.height)
		m.parameterSizesTable.SetWidth(m.width)
		m.parameterSizesTable.SetHeight(m.height)
	case spinner.TickMsg:
		var spCmd tea.Cmd
		m.spinner, spCmd = m.spinner.Update(msg)
		return m, spCmd
	}

	if m.viewMode == ConfirmDeleteView {
		updatedModel, formCmd := m.confirmForm.Update(msg)
		m.confirmForm = updatedModel.(*huh.Form)

		if m.confirmForm.State == huh.StateCompleted {
			m.viewMode = ModelView
			modelName := m.confirmDeleteModelName
			m.confirmDeleteModelName = ""
			if m.confirmResult {
				return m, deleteModelCmd(modelName)
			} else {
				return m, fetchModelsCmd()
			}
		}

		return m, formCmd
	}

	if m.formActive {
		updatedModel, formCmd := m.configForm.Update(msg)
		m.configForm = updatedModel.(*huh.Form)

		if m.configForm.State == huh.StateCompleted {
			m.formActive = false
			m.viewMode = ChatView
			m.textarea.Focus()
			m.updateViewport()
			return m, nil
		}

		return m, formCmd
	}

	switch m.viewMode {
	case InsertView:
		m.textarea, cmd = m.textarea.Update(msg)
	case ModelView:
		m.modelTable, cmd = m.modelTable.Update(msg)
	case AvailableModelsView:
		m.availableTable, cmd = m.availableTable.Update(msg)
	case ParameterSizesView:
		m.parameterSizesTable, cmd = m.parameterSizesTable.Update(msg)
	case DownloadingView:
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}

	return m, cmd
}

func (m model) View() string {
	return m.ViewWithoutError()
}

func (m model) ViewWithoutError() string {
	if m.viewMode == ModelView {
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
	}

	if m.formActive {
		return m.configForm.View()
	}

	switch m.viewMode {
	case ConfirmDeleteView:
		message := fmt.Sprintf("Are you sure you want to delete model '%s'? This action cannot be undone.", m.confirmDeleteModelName)
		return message + "\n\n" + m.confirmForm.View()
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

func fetchModelsCmd() tea.Cmd {
	return func() tea.Msg {
		models, err := fetchModels()
		if err != nil {
			return errMsg(err)
		}
		return modelsMsg(models)
	}
}

func (m *model) populateModelTable(models []OllamaModel) {
	var rows []table.Row

	rows = append(rows, table.Row{"Add New Model", "", ""})

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
	m.parameterSizesTable = table.New(
		table.WithColumns(columns),
		table.WithRows(rows),
		table.WithFocused(true),
	)
	m.parameterSizesTable.SetWidth(m.width)
	m.parameterSizesTable.SetHeight(m.height)
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
	apiURL := "http://localhost:11434/api/delete"

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

func sendChatMessage(m *model) tea.Cmd {
	return func() tea.Msg {
		if m.currentUserMessage == "" {
			return nil
		}

		m.conversationHistory = append(m.conversationHistory, map[string]string{
			"role":    "user",
			"content": m.currentUserMessage,
		})

		messagesForAssistant := make([]map[string]string, len(m.conversationHistory))
		copy(messagesForAssistant, m.conversationHistory)

		code, err := generateCode(messagesForAssistant, m)
		if err != nil {
			return errMsg(err)
		}

		m.conversationHistory = append(m.conversationHistory, map[string]string{
			"role":    "assistant",
			"content": code,
		})

		messagesForTester := make([]map[string]string, len(m.conversationHistory))
		copy(messagesForTester, m.conversationHistory)

		testResponse, err := testCode(messagesForTester, m)
		if err != nil {
			return errMsg(err)
		}

		m.conversationHistory = append(m.conversationHistory, map[string]string{
			"role":    "tester",
			"content": testResponse,
		})

		m.assistantResponses = append(m.assistantResponses, code)
		m.testerResponses = append(m.testerResponses, testResponse)
		m.userMessages = append(m.userMessages, m.currentUserMessage)
		m.currentUserMessage = ""
		m.updateViewport()

		return responseMsg(testResponse)
	}
}

func (m *model) updateViewport() {
	var conversation strings.Builder
	for _, msg := range m.conversationHistory {
		conversation.WriteString(fmt.Sprintf("**%s:**\n\n%s\n\n", msg["role"], msg["content"]))
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

func requestOllama(messages []map[string]string, config ChatConfig) (string, error) {
	apiURL := "http://localhost:11434/api/chat"

	numCtx, err := strconv.Atoi(config.Tokens)
	if err != nil || numCtx <= 0 {
		numCtx = 16384
	}

	options := map[string]interface{}{
		"num_ctx": numCtx,
	}

	requestBody, err := json.Marshal(map[string]interface{}{
		"model":    config.ModelVersion,
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

func fetchModels() ([]OllamaModel, error) {
	apiURL := "http://localhost:11434/api/tags"

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

func generateCode(messages []map[string]string, m *model) (string, error) {
	contextBytes, err := os.ReadFile(m.config.ContextFilePath)
	if err != nil {
		return "", fmt.Errorf("failed to read context file: %w", err)
	}
	context := string(contextBytes)

	systemPrompt := strings.ReplaceAll(m.config.SystemPrompt, "{context}", context)

	systemMessage := map[string]string{
		"role":    "system",
		"content": systemPrompt,
	}
	messagesWithSystem := append([]map[string]string{systemMessage}, messages...)

	return requestOllama(messagesWithSystem, m.config)
}

func testCode(messages []map[string]string, m *model) (string, error) {
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

	return requestOllama(messagesForTester, m.config)
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

func main() {
	model := InitialModel()
	p := tea.NewProgram(model)
	if _, err := p.Run(); err != nil {
		os.Exit(1)
	}
}
