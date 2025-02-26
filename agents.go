package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
)

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

func saveAgentsCmd(m *model) tea.Cmd {
	return func() tea.Msg {
		err := saveAgents(m)
		if err != nil {
			return errMsg(fmt.Errorf("failed to save agents: %w", err))
		}
		return notifyMsg("Agents reordered and saved successfully.")
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

func deleteAgentCmd(agentRole string) tea.Cmd {
	return func() tea.Msg {
		return agentDeletedMsg{Role: agentRole}
	}
}
