# agentui: Client for configurable agentic workflows in the terminal

agentui is a TUI client for managing AI agents, conducting multi-agent conversations, and interacting with local LLMs through Ollama. Designed for developers working with AI workflows, it combines chat functionality with agent management inside a terminal.

🚧 Work in Progress: This project is under active development. Some features are incomplete or experimental, and bugs are expected.

## Features

**🤖 AI Agent Management**

- Create and sequence specialized agents with custom roles
- Configurable agents for your specific needs
- Tool integration system (e.g., code checking)

**💬 Chat System**

- Persistent chat history with project organization
- Markdown rendering in the terminal with

**🛠️ Model Management**

- Browse Ollama model library
- Install/delete models directly

**⚙️ Technical Features**

- Terminal UI with responsive design
- Local data persistence
- Vim-like keyboard shortcuts

## Getting Started

### Dependencies

- Go
- Ollama
- Terminal emulator supporting UTF-8

### Running

```bash
git clone https://github.com/CVBERSCAPE/agentui.git
cd agentui
go run .
```

## Usage

### Key Bindings

| **Context**        | **Key**  | **Action**                                              |
| ------------------ | -------- | ------------------------------------------------------- |
| **Global**         | `Ctrl+Z` | Exit application                                        |
|                    | `Esc`    | Return to the previous view (usually back to Chat View) |
| **Chat View**      | `i`      | Enter message input (Insert Mode)                       |
|                    | `l`      | Open chat list                                          |
|                    | `m`      | Open model view                                         |
|                    | `g`      | Open agent view                                         |
|                    | `f`      | Open file picker _(Work in Progress)_                   |
|                    | `o`      | Toggle Ollama server                                    |
|                    | `j` / ↓  | Scroll down                                             |
|                    | `k` / ↑  | Scroll up                                               |
| **Insert View**    | `Enter`  | Send message                                            |
|                    | `Esc`    | Exit insert mode                                        |
| **Chat List View** | `Enter`  | Select/create new chat                                  |
|                    | `/`      | Search chats                                            |
| **Model View**     | `Enter`  | Select model in table                                   |
|                    | `d`      | Delete hovered model                                    |
| **Agent View**     | `Enter`  | Add/edit agent (depending on selection)                 |
|                    | `a`      | Add new agent                                           |
|                    | `e`      | Edit selected agent                                     |
|                    | `d`      | Delete agent                                            |
|                    | `u`      | Move hovered agent up in the chain                      |
|                    | `y`      | Move hovered agent down in the chain                    |

### Basic Workflow

1. **Start Ollama**: Press `o` to toggle Ollama service
2. **Create Agents**:
   - Press `g` to enter Agent View
   - Use `a` to add new agents with custom roles
3. **Start Chatting**:
   - Press `i` to compose messages
   - Agents process input sequentially
4. **Manage Models**:
   - Press `m` to browse/install models
   - Enter to select, `d` to delete

## Use Cases

**👩💻 Code Collaboration**

- Chain code generator + tester agents
- Integrated Go code checking tool
- Context-aware programming assistance

**🔄 Multi-Agent Workflows**

- Sequential processing pipelines
- Specialized agent roles, for example: research, analysis and summarization

## Future Roadmap

- 🖼️ Multi-modal support
- 🔌 Bindings for external tools
- 🤖 More robust agent configuration
- ⚡ Customizable agent processing with dynamic, user-defined signals
- 🚀 Performance optimizations
- 🛠 Quality-of-Life Enhancements
- 🎨 A more refined and aesthetic interface

## Configuration

Persistent data stored in project root for now for testing purposes, will be changed to .config/ at a latertime:

- `agents.json`: Agent configurations
- `chats/`: Chat history files
