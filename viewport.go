package main

import (
	"fmt"
	"log"
	"os"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	runningIndicatorColor = lipgloss.Color("#00FF00")
	stoppedIndicatorColor = lipgloss.Color("#FF0000")
	errorStyle            = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FF0000")).
				Bold(true)
)

var docStyle = lipgloss.NewStyle().Margin(1, 2)

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

func loadFileContext(filePath string) (string, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", filePath, err)
	}
	return string(content), nil
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

func sendChatMessage(m *model) tea.Cmd {
	return func() tea.Msg {
		if m.currentUserMessage == "" {
			log.Println("No user message to send.")
			return nil
		}

		m.conversationHistory = append(m.conversationHistory, map[string]string{
			"role":    "user",
			"content": m.currentUserMessage,
		})

		if len(m.agents) == 0 {
			return errMsg(fmt.Errorf("no agents configured"))
		}

		var lastResponse string
		currentInput := m.currentUserMessage

		for _, agent := range m.agents {
			response, err := processAgentChain(currentInput, m, agent)
			if err != nil {
				return errMsg(fmt.Errorf("error processing agent '%s': %w", agent.Role, err))
			}
			lastResponse = response
			currentInput = response

			m.conversationHistory = append(m.conversationHistory, map[string]string{
				"role":    "assistant",
				"content": response,
			})
		}

		m.assistantResponses = append(m.assistantResponses, lastResponse)
		m.userMessages = append(m.userMessages, m.currentUserMessage)
		m.currentUserMessage = ""
		m.loading = false
		m.viewMode = ChatView
		m.textarea.Blur()
		m.updateViewport()

		if err := m.saveCurrentChat(); err != nil {
			return errMsg(fmt.Errorf("failed to save chat: %w", err))
		}

		return responseMsg("Conversation processed successfully.")
	}
}
