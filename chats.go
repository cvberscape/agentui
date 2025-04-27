package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
)

func newChatDelegate() chatDelegate {
	d := chatDelegate{}

	d.styles.normal = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Padding(0, 0, 0, 2).
		MarginBottom(1)

	d.styles.selected = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#000000")).
		Background(lipgloss.Color("#00FF00")).
		Padding(0, 0, 0, 2).
		MarginBottom(1)

	return d
}

func (d chatDelegate) Height() int {
	return 3
}

func (d chatDelegate) Spacing() int {
	return 0
}

func (d chatDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd {
	return nil
}

func (d chatDelegate) Render(w io.Writer, m list.Model, index int, listItem list.Item) {
	i, ok := listItem.(chatItem)
	if !ok {
		return
	}

	title := i.Title()
	desc := i.Description()

	str := fmt.Sprintf("%s\n%s", title, desc)

	fn := d.styles.normal.Render
	if index == m.Index() {
		fn = d.styles.selected.Render
	}

	fmt.Fprint(w, fn(str))
}

func (i chatItem) FilterValue() string {
	return i.chat.Name
}

func (i chatItem) Title() string {
	return i.chat.Name
}

func (i chatItem) Description() string {
	return fmt.Sprintf("Project: %s | Created: %s | Messages: %d",
		i.chat.ProjectName,
		i.chat.CreatedAt.Format("2006-01-02 15:04:05"),
		len(i.chat.Messages))
}

func (m *model) initializeChatList() error {
	if err := os.MkdirAll(m.chatsFolderPath, 0755); err != nil {
		return fmt.Errorf("failed to create chats directory: %w", err)
	}

	chats, err := loadChats(m.chatsFolderPath)
	if err != nil {
		return fmt.Errorf("failed to load chats: %w", err)
	}

	items := make([]list.Item, 0, len(chats)+2)
	items = append(items, chatItem{Chat{Name: "Temporary Chat", ProjectName: ""}})
	items = append(items, chatItem{Chat{Name: "Create New Chat", ProjectName: ""}})
	for _, chat := range chats {
		items = append(items, chatItem{chat})
	}

	delegate := newChatDelegate()
	m.chatList = list.New(items, delegate, m.width, m.height-4)
	m.chatList.Title = "Chat List"
	m.chatList.SetShowStatusBar(false)
	m.chatList.SetFilteringEnabled(true)
	m.chatList.Styles.Title = lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#666666")).
		Padding(0, 1)

	m.chatList.Styles.NoItems = lipgloss.NewStyle().Margin(1, 2)
	m.chatList.SetSize(m.width, m.height-4)

	return nil
}

func (m *model) updateChatList(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		headerHeight := 3
		m.chatList.SetSize(msg.Width-2, msg.Height-headerHeight)
		return m, nil

	case tea.KeyMsg:
		if keyIsCtrlZ(msg) {
			return m, tea.Quit
		}

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
				} else if chatItem.chat.Name == "Temporary Chat" {
					// Create new temporary chat
					tempChat := Chat{
						ID:          "temp-" + uuid.New().String(),
						Name:        "Temporary Chat",
						ProjectName: "Temporary",
						CreatedAt:   time.Now(),
						Messages:    make([]map[string]string, 0),
					}
					m.selectedChat = &tempChat
					m.conversationHistory = tempChat.Messages
					m.viewMode = ChatView
					m.updateViewport()
					return m, nil
				}

				m.selectedChat = &chatItem.chat
				m.conversationHistory = chatItem.chat.Messages
				m.viewMode = ChatView
				m.updateViewport()
				return m, nil
			}
		}
	}

	m.chatList, cmd = m.chatList.Update(msg)
	return m, cmd
}

func (m *model) createNewChat(name string, projectName string) error {
	chat := createNewChat(name, projectName)

	if err := saveChat(chat, m.chatsFolderPath); err != nil {
		return fmt.Errorf("failed to save new chat: %w", err)
	}

	m.chatList.InsertItem(1, chatItem{chat})

	m.selectedChat = &chat
	m.conversationHistory = []map[string]string{}
	m.viewMode = ChatView

	return nil
}

func loadChats(folderPath string) ([]Chat, error) {
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

func (m *model) saveCurrentChat() error {
	if m.selectedChat == nil {
		return fmt.Errorf("no chat selected")
	}

	if strings.HasPrefix(m.selectedChat.ID, "temp-") {
		return nil
	}

	m.selectedChat.Messages = m.conversationHistory

	return saveChat(*m.selectedChat, m.chatsFolderPath)
}
