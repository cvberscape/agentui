package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/huh"
)

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

	tokenOptions := []huh.Option[string]{
		huh.NewOption("2048 tokens", "2048"),
		huh.NewOption("4096 tokens", "4096"),
		huh.NewOption("8192 tokens", "8192"),
		huh.NewOption("16384 tokens", "16384"),
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

			huh.NewSelect[string]().
				Title("Token Limit").
				Options(tokenOptions...).
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
