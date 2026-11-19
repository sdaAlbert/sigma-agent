package agent

import (
	"encoding/json"
	"strings"
)

const (
	RiskReadOnly        = "read_only"
	RiskWorkspaceWrite  = "workspace_write"
	RiskNetwork         = "network"
	RiskExternalService = "external_service"
	RiskCommitPublish   = "commit_publish"
)

// ClassifyToolAuthorization assigns a conservative, reviewable risk label to a
// registered tool call. Shell classification is heuristic; Docker/network
// controls remain the enforcement mechanism for command execution.
func ClassifyToolAuthorization(name string, input json.RawMessage, networkEnabled bool) AuthorizationRequest {
	var args map[string]any
	_ = json.Unmarshal(input, &args)
	stringArg := func(key string) string {
		value, _ := args[key].(string)
		return strings.TrimSpace(value)
	}

	request := AuthorizationRequest{Tool: name, Args: authorizationArgs(input)}
	request.Target = stringArg("path")
	if request.Target == "" {
		request.Target = stringArg("working_directory")
	}
	if request.Target == "" {
		request.Target = stringArg("base_image")
	}
	if request.Target == "" {
		request.Target = stringArg("action")
	}

	switch name {
	case "workspace_search", "read_file", "read_tool_output", "read_skill", "search", "search_memory", "search_session_history", "search_skills", "compact_context":
		request.RiskClass = RiskReadOnly
		request.Reason = "Reads workspace/session data without modifying the repository."
	case "git":
		action := stringArg("action")
		request.Target = action
		if rawPaths, ok := args["paths"]; ok {
			if encoded, err := json.Marshal(rawPaths); err == nil {
				request.Target += " " + truncateAuthorizationText(string(encoded), 240)
			}
		}
		switch action {
		case "status", "diff", "log", "show", "branches":
			request.RiskClass = RiskReadOnly
			request.Reason = "Inspects local Git data without changing the repository."
		case "commit":
			request.RiskClass = RiskCommitPublish
			request.Reason = "Creates a local Git commit; this built-in tool does not push to a remote."
		default:
			request.RiskClass = RiskWorkspaceWrite
			request.Reason = "Changes repository or Git metadata in the selected workspace."
		}
	case "create_file", "edit_file", "create_skill", "record_change_contract":
		request.RiskClass = RiskWorkspaceWrite
		request.Reason = "Writes repository files or task-local change state."
	case "prepare_environment":
		request.RiskClass = RiskExternalService
		request.Reason = "Builds or selects a Docker image and changes the local Docker image/cache state."
	case "run_command":
		command := stringArg("command")
		request.Target = truncateAuthorizationText(command, 240)
		lower := strings.ToLower(command)
		switch {
		case containsAuthorizationFragment(lower, "git push", "gh pr create", "gh release create", "npm publish", "docker push", "twine upload"):
			request.RiskClass = RiskCommitPublish
			request.Reason = "Runs a command that appears to publish or push repository artifacts to a remote service."
		case networkEnabled || containsAuthorizationFragment(lower, "curl ", "wget ", "git clone", "go get", "npm install", "pip install", "uv sync", "cargo install"):
			request.RiskClass = RiskNetwork
			request.Reason = "Runs a shell command that can access external networks or download dependencies."
		default:
			request.RiskClass = RiskWorkspaceWrite
			request.Reason = "Runs an arbitrary shell command that can modify the workspace or container state."
		}
	default:
		request.RiskClass = RiskExternalService
		request.Reason = "Tool origin is not a built-in read/write operation; treat it as an external integration."
		if strings.HasPrefix(name, "mcp__") || request.Target == "" {
			request.Target = name
		}
	}
	if request.Target == "" {
		request.Target = name
	}
	return request
}

func authorizationArgs(input json.RawMessage) json.RawMessage {
	const maxArgsBytes = 4096
	if len(input) <= maxArgsBytes {
		return append(json.RawMessage(nil), input...)
	}
	return json.RawMessage(`{"truncated":true}`)
}

func containsAuthorizationFragment(value string, fragments ...string) bool {
	for _, fragment := range fragments {
		if strings.Contains(value, fragment) {
			return true
		}
	}
	return false
}

func truncateAuthorizationText(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}
