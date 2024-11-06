package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/table"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

type (
	responseMsg string
	errMsg      error
	modelsMsg   []OllamaModel
)

type viewMode int

const (
	ChatView viewMode = iota
	InsertView
	ModelView
)

var (
	runningIndicatorColor = lipgloss.Color("#00FF00")
	stoppedIndicatorColor = lipgloss.Color("#FF0000")
)

func (m *model) indicatorStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FF0000")).
		Background(lipgloss.Color("#000000")).
		Border(lipgloss.HiddenBorder()).
		Padding(0)
}

type model struct {
	userMessages       []string
	assistantResponses []string
	testerResponses    []string
	currentUserMessage string
	err                error
	textarea           textarea.Model
	viewport           viewport.Model
	modelTable         table.Model
	width              int
	height             int
	loading            bool
	viewMode           viewMode
	renderer           *glamour.TermRenderer
	ollamaRunning      bool
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

	columns := []table.Column{
		{Title: "Name", Width: 20},
		{Title: "Parameter Size", Width: 15},
		{Title: "Size (GB)", Width: 10},
	}

	modelTable := table.New(
		table.WithColumns(columns),
		table.WithFocused(true),
	)

	m := &model{
		userMessages:       make([]string, 0),
		assistantResponses: make([]string, 0),
		testerResponses:    make([]string, 0),
		currentUserMessage: "",
		textarea:           ta,
		viewport:           vp,
		modelTable:         modelTable,
		renderer:           renderer,
		viewMode:           ChatView,
		ollamaRunning:      false,
	}

	m.updateTextareaIndicatorColor()
	return m
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, tea.EnterAltScreen, fetchModelsCmd())
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
		m.updateViewport()
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

		case "esc":
			switch m.viewMode {
			case InsertView:
				m.viewMode = ChatView
				m.textarea.Blur()
			case ModelView:
				m.viewMode = ChatView
			}
			return m, nil

		case "enter":
			if m.viewMode == InsertView {
				m.currentUserMessage = m.textarea.Value()
				m.textarea.Reset()
				m.loading = true
				m.viewMode = ChatView
				m.textarea.Blur()
				return m, sendChatMessage(m)
			}

		case "j":
			if m.viewMode == ModelView {
				m.modelTable.MoveDown(1)
			} else if m.viewMode == ChatView {
				m.viewport.LineDown(1)
			}

		case "k":
			if m.viewMode == ModelView {
				m.modelTable.MoveUp(1)
			} else if m.viewMode == ChatView {
				m.viewport.LineUp(1)
			}
		}

	case modelsMsg:
		m.populateModelTable(msg)

	case errMsg:
		m.loading = false
		m.err = msg
		m.updateViewport()
		return m, nil

	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.textarea.SetWidth(m.width)
		m.viewport.Width = m.width
		m.viewport.Height = m.height - 3
		m.updateViewport()
		m.viewport.GotoBottom()
	}

	switch m.viewMode {
	case InsertView:
		m.textarea, cmd = m.textarea.Update(msg)
	case ModelView:
		m.modelTable, cmd = m.modelTable.Update(msg)
	}

	return m, cmd
}

func (m model) View() string {
	status := "Ollama Serve: "
	if m.ollamaRunning {
		status += "Running"
	} else {
		status += "Stopped"
	}

	indicator := m.indicatorStyle().Render(status)

	switch m.viewMode {
	case ModelView:
		return indicator + "\n" + m.modelTable.View()
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
	for _, mdl := range models {
		rows = append(rows, table.Row{
			mdl.Name,
			mdl.Details.ParameterSize,
			FormatSizeGB(mdl.Size),
		})
	}
	m.modelTable.SetRows(rows)
}

func sendChatMessage(m *model) tea.Cmd {
	return func() tea.Msg {
		if m.currentUserMessage == "" {
			return nil
		}

		code, err := generateCode(m.currentUserMessage)
		if err != nil {
			return errMsg(err)
		}

		testResponse, err := testCode(code)
		if err != nil {
			return errMsg(err)
		}

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
	for i := 0; i < len(m.userMessages); i++ {
		conversation.WriteString("**User:**\n\n" + m.userMessages[i] + "\n\n")
		if i < len(m.assistantResponses) {
			conversation.WriteString("**Assistant:**\n\n" + m.assistantResponses[i] + "\n\n")
		}
		if i < len(m.testerResponses) {
			conversation.WriteString("**Tester:**\n\n" + m.testerResponses[i] + "\n\n")
		}
	}

	renderedContent, err := m.renderer.Render(conversation.String())
	if err != nil {
		fmt.Printf("Error rendering content: %v\n", err)
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

func requestOllama(messages []map[string]string, model string, agentType string) (string, error) {
	apiURL := "http://localhost:11434/api/chat"

	var systemMessage map[string]string

	switch agentType {
	case "assistant":
		systemMessage = map[string]string{
			"role":    "system",
			"content": "You are an assistant tasked with generating code based on the user's prompt. Be creative and provide the best solution possible. Your response should always be a full code file and no text explaining your decisions, i repeat your responses do NOT contain plaintext",
		}
	case "tester":
		systemMessage = map[string]string{
			"role":    "system",
			"content": "You are a code tester tasked with reviewing the generated code for potential bugs or issues. Identify and highlight any issues or improvements needed.",
		}
	default:
		systemMessage = map[string]string{
			"role":    "system",
			"content": "You are a helpful assistant. Please respond concisely and professionally.",
		}
	}

	messages = append([]map[string]string{systemMessage}, messages...)

	requestBody, err := json.Marshal(map[string]interface{}{
		"model":    model,
		"messages": messages,
		"stream":   false,
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(requestBody))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("error: %v", resp.Status)
	}

	var rawResponse map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&rawResponse); err != nil {
		return "", err
	}

	if message, ok := rawResponse["message"].(map[string]interface{}); ok {
		if content, ok := message["content"].(string); ok {
			return content, nil
		}
	}

	return "", fmt.Errorf("unexpected response format: %+v", rawResponse)
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

func generateCode(request string) (string, error) {
	messages := []map[string]string{
		{"role": "user", "content": request},
	}
	return requestOllama(messages, "llama3.1", "assistant")
}

func testCode(code string) (string, error) {
	messages := []map[string]string{
		{"role": "user", "content": code},
	}
	return requestOllama(messages, "llama3.2", "tester")
}

type ClipboardBackend int

const (
	Wayland ClipboardBackend = iota
	X11
	TmuxTTY
	Unknown
)

func detectClipboardBackend() ClipboardBackend {
	if os.Getenv("WAYLAND_DISPLAY") != "" {
		return Wayland
	}
	if os.Getenv("DISPLAY") != "" {
		return X11
	}
	if os.Getenv("TMUX") != "" && os.Getenv("DISPLAY") == "" {
		return TmuxTTY
	}
	return Unknown
}

func copyToClipboard(text string) error {
	switch detectClipboardBackend() {
	case Wayland:
		return copyToWaylandClipboard(text)
	case X11:
		return copyToX11Clipboard(text)
	case TmuxTTY:
		return copyToTmuxClipboard(text)
	default:
		return fmt.Errorf("unsupported clipboard environment")
	}
}

func copyToWaylandClipboard(text string) error {
	cmd := exec.Command("wl-copy")
	cmd.Stdin = bytes.NewBufferString(text)
	return cmd.Run()
}

func copyToX11Clipboard(text string) error {
	cmd := exec.Command("xclip", "-selection", "clipboard")
	cmd.Stdin = bytes.NewBufferString(text)
	return cmd.Run()
}

func copyToTmuxClipboard(text string) error {
	loadCmd := exec.Command("tmux", "load-buffer", "-")
	loadCmd.Stdin = bytes.NewBufferString(text)
	return loadCmd.Run()
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

func main() {
	model := InitialModel()
	p := tea.NewProgram(model)
	if _, err := p.Run(); err != nil {
		os.Exit(1)
	}
}
