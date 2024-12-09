# agentui

## TODO

- support for multimodal models // WIP
- more robust agent customization (api parameters)
- bindings for adding external tools (bash or python scripts etc)
- customizable signals for agent processing
- efficiency/QOL features for chat interface
- more refined/aesthetic UI
- optimizations
- should probably modularize the code to make it more readable rather than having 2.7k lines of code slapped into a single main.go file, i just prefer it this way for now

## Running the app

```sh
git clone https://github.com/CVBERSCAPE/agentui.git
```

```sh
cd agentui
```

```sh
go mod tidy
```

```sh
go run .
```

should work out of the box on any unix system but untested, tested system config: artix linux and alacritty/kitty as terminal emulators

currently all persistent data (chats, agent configs) are saved at the root dir of the project, this is easier for now for testing purposes, will be changed to persist at .config at some point

the app also contains quite a bit of logging code for now which might obstruct the ui on occasion

## Dependencies

- Go (make)
- Ollama
- golangci-lint (Optional, for the go linter tool)

## Keybindings

    Global
        Ctrl+Z: exits the app
        ESC: works as a return (usually ends up back in chat view)
        
    Chat View
        i: enter insert mode to send messages
        l: open chat list
        m: open model view
        g: open agent view
        f: open file picker (WORK IN PROGRESS)
        o: toggle ollama server
        j / Down Arrow: scroll down
        k / Up Arrow: scroll up

    Insert View (when typing a user message)
        Enter: send message
        ESC: exit insert mode

    Chat List View
        Enter: select/create new chat
        /: search

    Model View
        Enter: works as selector in table
        d: delete hovered model

    Agent View
        Enter: add/edit agent depending on hovered selection
        a: add new agent
        e: edit selected agent
        d: delete agent
        u: move hovered agent up 1 entry in the table (agents are processed sequentially, from the top of the table to the bottom)
        y: move hovered agent down 1 entry in the table (agents are processed sequentially, from the top of the table to the bottom)

## Observations

For now the best performing model for coding/tool tasks is gwen2.5-coder, other tested models like llama3.1 at lower parameter sizes are quite underwhelming, especially for tool usage. this *should* change when deepseek r1 is released as an oss model
