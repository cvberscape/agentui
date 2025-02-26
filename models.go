package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *model) refreshModelView() tea.Cmd {
	return tea.Sequence(
		func() tea.Msg {
			currentCursor := m.modelTable.Cursor()

			models, err := fetchModels()
			if err != nil {
				return errMsg(err)
			}

			m.populateModelTable(models)

			if currentCursor < len(m.modelTable.Rows()) {
				m.modelTable.SetCursor(currentCursor)
			} else {
				if len(m.modelTable.Rows()) > 0 {
					m.modelTable.SetCursor(len(m.modelTable.Rows()) - 1)
				}
			}

			return modelsMsg(models)
		},
	)
}

func fetchModelsCmd() tea.Cmd {
	return func() tea.Msg {
		models, err := fetchModels()
		if err != nil {
			// Return an empty modelsMsg to ensure the table still renders
			return modelsMsg{}
		}
		return modelsMsg(models)
	}
}

func (m *model) populateModelTable(models []OllamaModel) {
	var rows []table.Row

	// Always add the "Add New Model" entry
	rows = append(rows, table.Row{"Add New Model", "N/A", "N/A"})

	// Add fetched models if available
	if len(models) > 0 {
		sort.Slice(models, func(i, j int) bool {
			return models[i].Name < models[j].Name
		})

		for _, mdl := range models {
			rows = append(rows, table.Row{
				mdl.Name,
				mdl.Details.ParameterSize,
				FormatSizeGB(mdl.Size),
			})
		}
	}

	// Set the table rows
	m.modelTable.SetRows(rows)

	// Ensure the table is focused and the cursor is set correctly
	if len(rows) > 0 {
		m.modelTable.SetCursor(0)
	}
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

func fetchAvailableModelsCmd() tea.Cmd {
	return func() tea.Msg {
		models, err := scrapeOllamaLibrary()
		if err != nil {
			return errMsg(err)
		}
		return availableModelsMsg(models)
	}
}

func downloadModelCmd(modelName string) tea.Cmd {
	return func() tea.Msg {
		if err := downloadModel(modelName); err != nil {
			return errMsg(fmt.Errorf("failed to download model: %w", err))
		}
		return modelDownloadedMsg(modelName)
	}
}
