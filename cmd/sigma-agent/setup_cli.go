package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sdaAlbert/sigma-agent/internal/agent"
	modelconfig "github.com/sdaAlbert/sigma-agent/internal/model"
)

type interactiveSetupOptions struct {
	repo          string
	sessionID     string
	resumeID      string
	pushBranch    string
	githubWrite   bool
	allowNetwork  bool
	githubMCP     bool
	initialPrompt string
	model         modelconfig.Selection
	modelExplicit bool
}

func interactiveSetup(options interactiveSetupOptions) (interactiveSetupOptions, bool, *bufio.Scanner, error) {
	options.repo = strings.TrimSpace(options.repo)
	if options.repo == "" {
		return options, false, nil, errors.New("interactive setup needs a repository path; use /repo <path>")
	}
	if options.model.Provider == "" {
		selection, err := modelconfig.LoadDefaultSelection()
		if err != nil {
			return options, false, nil, err
		}
		options.model = selection
	}
	if options.model.Provider == "" {
		options.model = modelconfig.Selection{Provider: "minimax", Model: modelconfig.DefaultModel("minimax")}
	}
	fmt.Fprintln(os.Stderr, "\nSigma setup — configure this session, then enter a task.")
	fmt.Fprintln(os.Stderr, "Commands: /login <provider|github>, /model [provider/model-id|save], /logout <provider|github>, /repo <path>, /session <id>, /resume <id>, /network on|off, /publish <branch>|off, /github-write on|off, /start, /help, /exit")
	printSetupStatus(options)

	scanner := bufio.NewScanner(os.Stdin)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for {
		fmt.Fprint(os.Stderr, "Sigma> ")
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return options, false, scanner, fmt.Errorf("read setup command: %w", err)
			}
			fmt.Fprintln(os.Stderr, "\nSetup cancelled.")
			return options, false, scanner, nil
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "/") {
			if err := validateSetupOptions(options); err != nil {
				fmt.Fprintln(os.Stderr, err)
				continue
			}
			options.initialPrompt = line
			return options, true, scanner, nil
		}

		command, value, _ := strings.Cut(line, " ")
		value = strings.TrimSpace(value)
		switch command {
		case "/help":
			printSetupHelp()
		case "/login":
			if strings.EqualFold(value, "github") {
				token, err := readSecret("Paste the GitHub personal access token (input hidden): ")
				if err != nil {
					fmt.Fprintf(os.Stderr, "Could not read GitHub token: %v\n", err)
					continue
				}
				if err := agent.SaveGitHubToken(token); err != nil {
					fmt.Fprintf(os.Stderr, "Could not save GitHub token: %v\n", err)
					continue
				}
				fmt.Fprintln(os.Stderr, "Saved GitHub token in the user configuration directory.")
				continue
			}
			provider, ok := modelconfig.NormalizeProvider(value)
			if !ok {
				fmt.Fprintln(os.Stderr, "Choose /login github or a model provider: minimax|anthropic|openai|deepseek|openrouter.")
				continue
			}
			key, err := readSecret("Paste the API key for " + provider + " (input hidden): ")
			if err != nil {
				fmt.Fprintf(os.Stderr, "Could not read API key: %v\n", err)
				continue
			}
			if err := modelconfig.SaveAPIKey(provider, key); err != nil {
				fmt.Fprintf(os.Stderr, "Could not save API key: %v\n", err)
				continue
			}
			fmt.Fprintf(os.Stderr, "Saved %s API key in the user configuration directory.\n", provider)
		case "/logout":
			if strings.EqualFold(value, "github") {
				if err := agent.RemoveGitHubToken(); err != nil {
					fmt.Fprintf(os.Stderr, "Could not remove saved GitHub token: %v\n", err)
					continue
				}
				fmt.Fprintln(os.Stderr, "Removed the saved GitHub token. Environment or project-local credentials can still be used.")
				continue
			}
			provider, ok := modelconfig.NormalizeProvider(value)
			if !ok {
				fmt.Fprintln(os.Stderr, "Usage: /logout github|minimax|anthropic|openai|deepseek|openrouter")
				continue
			}
			if err := modelconfig.RemoveAPIKey(provider); err != nil {
				fmt.Fprintf(os.Stderr, "Could not remove saved API key: %v\n", err)
				continue
			}
			fmt.Fprintf(os.Stderr, "Removed the saved %s API key. Any configured environment variable can still provide credentials.\n", provider)
		case "/model":
			if value == "" || value == "list" {
				printModelOptions(options.model)
				continue
			}
			if value == "save" {
				hasKey, keyErr := modelconfig.ProviderHasAPIKey(options.model.Provider)
				if keyErr != nil || !hasKey {
					fmt.Fprintf(os.Stderr, "Configure a %s API key before saving this as the default.\n", options.model.Provider)
					continue
				}
				if err := modelconfig.SaveDefaultSelection(options.model); err != nil {
					fmt.Fprintf(os.Stderr, "Could not save default model: %v\n", err)
				} else {
					fmt.Fprintf(os.Stderr, "Saved default model %s/%s.\n", options.model.Provider, options.model.Model)
				}
				continue
			}
			providerText, modelID, hasModel := strings.Cut(value, "/")
			provider, ok := modelconfig.NormalizeProvider(providerText)
			if !ok {
				fmt.Fprintln(os.Stderr, "Usage: /model <provider>/<model-id> (see /model list)")
				continue
			}
			if !hasModel {
				modelID = modelconfig.DefaultModel(provider)
			}
			if strings.TrimSpace(modelID) == "" {
				fmt.Fprintf(os.Stderr, "Enter a model id: /model %s/<model-id>\n", provider)
				continue
			}
			hasKey, err := modelconfig.ProviderHasAPIKey(provider)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Could not inspect saved model credentials: %v\n", err)
				continue
			}
			if !hasKey {
				fmt.Fprintf(os.Stderr, "No %s API key is configured. Use /login %s first.\n", provider, provider)
				continue
			}
			options.model = modelconfig.Selection{Provider: provider, Model: strings.TrimSpace(modelID)}
			options.modelExplicit = true
			fmt.Fprintf(os.Stderr, "Selected model %s/%s for this session. Use /model save to make it the default.\n", provider, modelID)
		case "/repo":
			if value == "" {
				fmt.Fprintln(os.Stderr, "Usage: /repo <path>")
				continue
			}
			repoPath, err := filepath.Abs(value)
			if err == nil {
				var top string
				top, err = gitOutput(repoPath, "rev-parse", "--show-toplevel")
				if err == nil {
					options.repo = strings.TrimSpace(top)
					fmt.Fprintf(os.Stderr, "Repository: %s\n", options.repo)
				}
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "Could not select repository: %v\n", err)
			}
		case "/session":
			if !agent.ValidSessionID(value) {
				fmt.Fprintln(os.Stderr, "Usage: /session <id> (letters, numbers, _ and -; up to 128 characters)")
				continue
			}
			options.sessionID = value
			options.resumeID = ""
			fmt.Fprintf(os.Stderr, "New session ID: %s\n", value)
		case "/resume":
			if !agent.ValidSessionID(value) {
				fmt.Fprintln(os.Stderr, "Usage: /resume <existing-session-id>")
				continue
			}
			options.resumeID = value
			options.sessionID = ""
			fmt.Fprintf(os.Stderr, "Will resume session: %s\n", value)
		case "/network":
			switch value {
			case "on":
				options.allowNetwork = true
			case "off":
				options.allowNetwork = false
			default:
				fmt.Fprintln(os.Stderr, "Usage: /network on|off")
				continue
			}
			printSetupStatus(options)
		case "/publish":
			if value == "off" {
				options.pushBranch = ""
				options.githubWrite = false
				fmt.Fprintln(os.Stderr, "Publishing disabled for this session.")
				continue
			}
			if value == "" || strings.HasPrefix(value, "-") || strings.TrimSpace(value) != value {
				fmt.Fprintln(os.Stderr, "Usage: /publish <new-feature-branch>|off")
				continue
			}
			checked, err := gitOutput(options.repo, "check-ref-format", "--branch", value)
			if err != nil || strings.TrimSpace(checked) != value {
				fmt.Fprintf(os.Stderr, "Invalid Git branch name: %s\n", value)
				continue
			}
			options.pushBranch = value
			fmt.Fprintf(os.Stderr, "Commit/push enabled for new branch %s. The branch is created when you start a new task.\n", value)
		case "/github-write":
			switch value {
			case "on":
				if !options.githubMCP {
					fmt.Fprintln(os.Stderr, "GitHub MCP is disabled; restart with GitHub MCP enabled before using PR creation.")
					continue
				}
				if options.pushBranch == "" {
					fmt.Fprintln(os.Stderr, "Set a publish branch first with /publish <branch>.")
					continue
				}
				if !agent.GitHubMCPConfigured() {
					fmt.Fprintln(os.Stderr, "Configure a GitHub token before enabling PR creation.")
					continue
				}
				options.githubWrite = true
			case "off":
				options.githubWrite = false
			default:
				fmt.Fprintln(os.Stderr, "Usage: /github-write on|off")
				continue
			}
			printSetupStatus(options)
		case "/start":
			if err := validateSetupOptions(options); err != nil {
				fmt.Fprintln(os.Stderr, err)
				continue
			}
			return options, true, scanner, nil
		case "/exit", "/quit":
			fmt.Fprintln(os.Stderr, "Setup cancelled.")
			return options, false, scanner, nil
		default:
			fmt.Fprintf(os.Stderr, "Unknown setup command %q. Use /help.\n", command)
		}
	}
}

func validateSetupOptions(options interactiveSetupOptions) error {
	if options.resumeID == "" || options.modelExplicit {
		hasKey, err := modelconfig.ProviderHasAPIKey(options.model.Provider)
		if err != nil {
			return fmt.Errorf("check model credentials: %w", err)
		}
		if !hasKey {
			return fmt.Errorf("no API key configured for %s; use /login %s before starting", options.model.Provider, options.model.Provider)
		}
	}
	if options.githubWrite && options.pushBranch == "" {
		return errors.New("PR creation requires a publish branch; use /publish <branch>")
	}
	if options.githubWrite && !options.githubMCP {
		return errors.New("PR creation requires GitHub MCP")
	}
	if options.githubWrite && !agent.GitHubMCPConfigured() {
		return errors.New("PR creation requires a configured GitHub token")
	}
	return nil
}

func printSetupHelp() {
	fmt.Fprintln(os.Stderr, `Configure Sigma before starting the Agent:
  /login <provider>    Save an API key or GitHub token (input is hidden)
  /model [provider/id] Show or select the model; /model save sets the default
  /logout <provider>   Remove a saved model key or GitHub token
  /repo <path>          Select a local Git repository
  /session <id>         Name a new saved session
  /resume <id>          Resume a saved session
  /network on|off       Allow/block container internet access (on by default)
  /publish <branch>     Enable commit and push to a new origin branch
  /publish off          Disable Git publishing
  /github-write on|off  Enable/disable GitHub pull request creation
  /start                Start an empty interactive conversation
  /exit                 Quit setup

You can also type a task directly to start it with the current settings.`)
}

func printModelOptions(current modelconfig.Selection) {
	fmt.Fprintf(os.Stderr, "Current selection: %s/%s\nAvailable providers:\n", current.Provider, current.Model)
	for _, provider := range modelconfig.Providers() {
		configured, err := modelconfig.ProviderHasAPIKey(provider.ID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  %-10s credential status unavailable: %v\n", provider.ID, err)
			continue
		}
		status := "key not configured"
		if configured {
			status = "ready"
		}
		fmt.Fprintf(os.Stderr, "  %-10s %-12s %s (%s)\n", provider.ID, provider.Name, status, provider.Environment)
	}
	fmt.Fprintln(os.Stderr, "Choose a model with /model <provider>/<model-id>; model IDs are entered directly and are not auto-discovered.")
}

func printSetupStatus(options interactiveSetupOptions) {
	resume := options.sessionID
	if options.resumeID != "" {
		resume = "resume " + options.resumeID
	}
	if resume == "" {
		resume = "automatic"
	}
	network := "off"
	if options.allowNetwork {
		network = "on"
	}
	publish := "off"
	if options.pushBranch != "" {
		publish = options.pushBranch
	}
	pr := "off"
	if options.githubWrite {
		pr = "on"
	}
	modelKey := "missing key"
	if configured, err := modelconfig.ProviderHasAPIKey(options.model.Provider); err == nil && configured {
		modelKey = "key ready"
	}
	fmt.Fprintf(os.Stderr, "Repo: %s | Session: %s | Model: %s/%s (%s) | Container network: %s | Publish: %s | GitHub PR: %s\n", options.repo, resume, options.model.Provider, options.model.Model, modelKey, network, publish, pr)
}
