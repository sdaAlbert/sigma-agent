package workspace

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestPrepareEnvironmentWithoutSetupPersistsImageWithoutBuilding(t *testing.T) {
	root := t.TempDir()
	state, err := newCommandEnvironmentState(root)
	if err != nil {
		t.Fatal(err)
	}
	tool := (workspace{root: root, commandEnvironment: state}).prepareEnvironmentTool()
	const image = "sigma-test/no-pull:latest"
	result, err := tool.Handle(context.Background(), json.RawMessage(`{"base_image":"`+image+`","setup_command":""}`))
	if err != nil {
		t.Fatalf("prepare_environment with empty setup command: %v", err)
	}
	_, payload, found := strings.Cut(result, "\n")
	var metadata struct {
		Status        string `json:"status"`
		PreparedImage string `json:"prepared_image"`
	}
	if !found || json.Unmarshal([]byte(payload), &metadata) != nil || metadata.Status != "prepared" || metadata.PreparedImage != image {
		t.Fatalf("prepare_environment result = %q, want prepared image metadata", result)
	}
	if state.Image() != image {
		t.Fatalf("selected image = %q, want %q", state.Image(), image)
	}
	reloaded, err := newCommandEnvironmentState(root)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Image() != image {
		t.Fatalf("reloaded image = %q, want persisted image %q", reloaded.Image(), image)
	}
}

func TestDockerEnvironmentBuildNetworkIsDisabledUnlessEnabled(t *testing.T) {
	offline := dockerEnvironmentBuildArgs("Dockerfile", "sigma-test/image:local", "context", false)
	if got := strings.Join(offline, " "); !strings.Contains(got, "--network none") {
		t.Fatalf("offline build args = %q, want --network none", got)
	}
	online := dockerEnvironmentBuildArgs("Dockerfile", "sigma-test/image:local", "context", true)
	if got := strings.Join(online, " "); strings.Contains(got, "--network none") {
		t.Fatalf("network-enabled build args = %q, must not force network none", got)
	}
}
