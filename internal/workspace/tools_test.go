package workspace

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTestPurposeCommandsPreserveExitStatus(t *testing.T) {
	tests := []struct {
		name    string
		command string
		wantErr bool
	}{
		{name: "plain test command", command: "python3 -m pytest tests/utils_tests/test_http.py"},
		{name: "setup chained with and", command: "cd /workspace && python3 -m unittest tests.test_http"},
		{name: "quoted pipe is data", command: `python3 -c "print('|')"`},
		{name: "pipeline can mask failure", command: "python3 -m pytest tests | tail -20", wantErr: true},
		{name: "fallback can mask failure", command: "python3 -m pytest tests || echo passed", wantErr: true},
		{name: "semicolon can mask failure", command: "python3 -m pytest tests; echo passed", wantErr: true},
		{name: "stderr redirection is safe", command: "python3 -m pytest tests 2>&1"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateTestCommandExitStatus(test.command)
			if test.wantErr && err == nil {
				t.Fatalf("validateTestCommandExitStatus(%q) = nil, want error", test.command)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("validateTestCommandExitStatus(%q) error = %v", test.command, err)
			}
		})
	}
}

// The command network can be kept internal when the operator disables egress.
func TestIsolatedTestNetworkIsInternalUnlessNetworkIsEnabled(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is not available in this environment")
	}
	off, err := createIsolatedTestNetwork(context.Background(), false)
	if err != nil {
		t.Fatalf("createIsolatedTestNetwork(false) error = %v", err)
	}
	defer removeIsolatedTestNetwork(off)
	inspect, err := exec.Command("docker", "network", "inspect", off).CombinedOutput()
	if err != nil {
		t.Fatalf("inspect %s: %v: %s", off, err, strings.TrimSpace(string(inspect)))
	}
	if !strings.Contains(string(inspect), `"Internal": true`) {
		t.Fatalf("network %s is not internal, want an internal network with no external egress", off)
	}
}

func TestIsolatedTestNetworkUsesBridgeWhenNetworkIsEnabled(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is not available in this environment")
	}
	on, err := createIsolatedTestNetwork(context.Background(), true)
	if err != nil {
		t.Fatalf("createIsolatedTestNetwork(true) error = %v", err)
	}
	defer removeIsolatedTestNetwork(on)
	inspect, err := exec.Command("docker", "network", "inspect", on).CombinedOutput()
	if err != nil {
		t.Fatalf("inspect %s: %v: %s", on, err, strings.TrimSpace(string(inspect)))
	}
	if !strings.Contains(string(inspect), `"Internal": false`) {
		t.Fatalf("network %s is internal, want a bridge network with egress when the session opted in", on)
	}
}

func TestCommandContainerNetworkUsesNoneWhenNetworkIsDisabled(t *testing.T) {
	network, err := commandContainerNetwork(context.Background(), false)
	if err != nil {
		t.Fatalf("commandContainerNetwork(false) error = %v", err)
	}
	if network != "none" {
		t.Fatalf("commandContainerNetwork(false) = %q, want none", network)
	}
	if err := removeIsolatedTestNetwork(network); err != nil {
		t.Fatalf("remove built-in none network: %v", err)
	}
}

func TestCommandContainerRunArgsUseReadOnlyRootAndBoundedScratch(t *testing.T) {
	args := containerRunArgs("sigma-test", "none")
	joined := strings.Join(args, " ")
	for _, expected := range []string{
		"--read-only",
		"/tmp:rw,exec,nosuid,nodev,size=256m",
		"/var/tmp:rw,exec,nosuid,nodev,size=128m",
		"--memory 2g",
		"--cpus 2",
		"--pids-limit 128",
		"--cap-drop ALL",
	} {
		if !strings.Contains(joined, expected) {
			t.Errorf("containerRunArgs() missing %q in %q", expected, joined)
		}
	}
}

func TestSharedContainerFilesystemArgsUseReadOnlyRoot(t *testing.T) {
	joined := strings.Join(commandContainerFilesystemArgs(), " ")
	if !strings.Contains(joined, "--read-only") {
		t.Fatalf("shared filesystem args = %q, want read-only root filesystem", joined)
	}
	if !strings.Contains(joined, "--tmpfs /tmp:") || !strings.Contains(joined, "--tmpfs /var/tmp:") {
		t.Fatalf("shared filesystem args = %q, want bounded temporary directories", joined)
	}
}

func TestDockerCommandContainerFilesystemIsolation(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is not available")
	}
	if output, err := exec.Command("docker", "info", "--format", "{{.ServerVersion}}").CombinedOutput(); err != nil {
		t.Skipf("Docker daemon is not available: %v: %s", err, strings.TrimSpace(string(output)))
	}
	const image = "debian:trixie-slim"
	if output, err := exec.Command("docker", "image", "inspect", image).CombinedOutput(); err != nil {
		t.Skipf("Docker image %s is not available locally: %v: %s", image, err, strings.TrimSpace(string(output)))
	}

	workspaceRoot := t.TempDir()
	containerName, err := newDockerContainerName("sigma-rootfs-test-")
	if err != nil {
		t.Fatal(err)
	}
	args := containerRunArgs(containerName, "none")
	args = append(args,
		"--volume", filepath.ToSlash(workspaceRoot)+":/workspace",
		"--workdir", "/workspace", image,
		"/bin/sh", "-lc",
		"if touch /etc/sigma-readonly-probe 2>/dev/null; then echo ROOT_WRITABLE; exit 41; else echo ROOT_READ_ONLY; fi; printf workspace-ok > /workspace/sigma-write-probe; printf tmp-ok > /tmp/sigma-write-probe; printf vartmp-ok > /var/tmp/sigma-write-probe",
	)
	output, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("Docker filesystem smoke run failed: %v: %s", err, strings.TrimSpace(string(output)))
	}
	if !strings.Contains(string(output), "ROOT_READ_ONLY") {
		t.Fatalf("container did not report a read-only root filesystem: %s", strings.TrimSpace(string(output)))
	}
	if contents, err := os.ReadFile(filepath.Join(workspaceRoot, "sigma-write-probe")); err != nil || string(contents) != "workspace-ok" {
		t.Fatalf("workspace bind mount write = %q, %v; want workspace-ok", contents, err)
	}
}

func TestDockerSessionContainerIsReusedBoundedAndCleanedUp(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is not available")
	}
	if output, err := exec.Command("docker", "info", "--format", "{{.ServerVersion}}").CombinedOutput(); err != nil {
		t.Skipf("Docker daemon is not available: %v: %s", err, strings.TrimSpace(string(output)))
	}
	const image = "debian:trixie-slim"
	if output, err := exec.Command("docker", "image", "inspect", image).CombinedOutput(); err != nil {
		t.Skipf("Docker image %s is not available locally: %v: %s", image, err, strings.TrimSpace(string(output)))
	}
	t.Setenv("SIGMA_HOST_SECRET_TEST", "must-not-enter-container")

	root := t.TempDir()
	session := &commandContainerSession{root: root, allowNetwork: false}
	t.Cleanup(func() { _ = session.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	first, _, err := session.run(ctx, image, "printf first", root)
	if err != nil || !strings.Contains(first, "first") {
		t.Fatalf("first session command = %q, %v", first, err)
	}
	container := session.container
	second, _, err := session.run(ctx, image, "printf second", root)
	if err != nil || !strings.Contains(second, "second") {
		t.Fatalf("second session command = %q, %v", second, err)
	}
	if session.container != container {
		t.Fatalf("session container changed from %q to %q between commands", container, session.container)
	}
	var hostConfig struct {
		ReadonlyRootfs bool     `json:"ReadonlyRootfs"`
		Memory         int64    `json:"Memory"`
		NanoCpus       int64    `json:"NanoCpus"`
		PidsLimit      int64    `json:"PidsLimit"`
		NetworkMode    string   `json:"NetworkMode"`
		CapDrop        []string `json:"CapDrop"`
		SecurityOpt    []string `json:"SecurityOpt"`
	}
	inspect, err := exec.Command("docker", "inspect", "--format", "{{json .HostConfig}}", container).CombinedOutput()
	if err != nil {
		t.Fatalf("inspect session container: %v: %s", err, strings.TrimSpace(string(inspect)))
	}
	if err := json.Unmarshal(inspect, &hostConfig); err != nil {
		t.Fatalf("decode container HostConfig %s: %v", inspect, err)
	}
	if !hostConfig.ReadonlyRootfs || hostConfig.Memory != 2<<30 || hostConfig.NanoCpus != 2_000_000_000 || hostConfig.PidsLimit != 128 || hostConfig.NetworkMode != "none" {
		t.Errorf("container restrictions = %#v, want read-only root, 2 GiB, 2 CPUs, 128 PIDs, network none", hostConfig)
	}
	if !containsTestString(hostConfig.CapDrop, "ALL") || !containsTestString(hostConfig.SecurityOpt, "no-new-privileges") {
		t.Errorf("container privilege restrictions = cap-drop %v security-opt %v", hostConfig.CapDrop, hostConfig.SecurityOpt)
	}
	environment, err := exec.Command("docker", "inspect", "--format", "{{json .Config.Env}}", container).CombinedOutput()
	if err != nil {
		t.Fatalf("inspect session environment: %v: %s", err, strings.TrimSpace(string(environment)))
	}
	if strings.Contains(string(environment), "SIGMA_HOST_SECRET_TEST") || strings.Contains(string(environment), "must-not-enter-container") {
		t.Fatalf("host environment leaked into container: %s", environment)
	}
	mountOutput, err := exec.Command("docker", "inspect", "--format", "{{json .Mounts}}", container).CombinedOutput()
	if err != nil {
		t.Fatalf("inspect session mounts: %v: %s", err, strings.TrimSpace(string(mountOutput)))
	}
	var mounts []struct {
		Type        string `json:"Type"`
		Destination string `json:"Destination"`
		RW          bool   `json:"RW"`
	}
	if err := json.Unmarshal(mountOutput, &mounts); err != nil {
		t.Fatalf("decode session mounts %s: %v", mountOutput, err)
	}
	bindMounts := 0
	for _, mount := range mounts {
		if mount.Type != "bind" {
			continue
		}
		bindMounts++
		if mount.Destination != "/workspace" || !mount.RW {
			t.Errorf("unexpected persistent host bind mount: %#v", mount)
		}
	}
	if bindMounts != 1 {
		t.Errorf("container has %d host bind mounts, want only the selected workspace", bindMounts)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("close session container: %v", err)
	}
	if _, err := exec.Command("docker", "inspect", container).CombinedOutput(); err == nil {
		t.Fatalf("session container %q remained after Close", container)
	}
}

func TestDockerSessionNetworkOptInIsDedicatedAndCleanedUp(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is not available")
	}
	if output, err := exec.Command("docker", "info", "--format", "{{.ServerVersion}}").CombinedOutput(); err != nil {
		t.Skipf("Docker daemon is not available: %v: %s", err, strings.TrimSpace(string(output)))
	}
	const image = "debian:trixie-slim"
	if output, err := exec.Command("docker", "image", "inspect", image).CombinedOutput(); err != nil {
		t.Skipf("Docker image %s is not available locally: %v: %s", image, err, strings.TrimSpace(string(output)))
	}
	session := &commandContainerSession{root: t.TempDir(), allowNetwork: true}
	t.Cleanup(func() { _ = session.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if output, _, err := session.run(ctx, image, "printf network-session", session.root); err != nil || !strings.Contains(output, "network-session") {
		t.Fatalf("network-enabled session command = %q, %v", output, err)
	}
	container, network := session.container, session.network
	if network == "" || network == "none" {
		t.Fatalf("network-enabled session network = %q, want a dedicated bridge", network)
	}
	mode, err := exec.Command("docker", "inspect", "--format", "{{.HostConfig.NetworkMode}}", container).CombinedOutput()
	if err != nil || strings.TrimSpace(string(mode)) != network {
		t.Fatalf("container network mode = %q, %v; want %q", strings.TrimSpace(string(mode)), err, network)
	}
	internal, err := exec.Command("docker", "network", "inspect", "--format", "{{.Internal}}", network).CombinedOutput()
	if err != nil || strings.TrimSpace(string(internal)) != "false" {
		t.Fatalf("opt-in Docker network Internal = %q, %v; want false", strings.TrimSpace(string(internal)), err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("close network-enabled session: %v", err)
	}
	if _, err := exec.Command("docker", "network", "inspect", network).CombinedOutput(); err == nil {
		t.Fatalf("session network %q remained after Close", network)
	}
}

func TestDockerSessionCancellationRemovesRunningContainer(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is not available")
	}
	if output, err := exec.Command("docker", "info", "--format", "{{.ServerVersion}}").CombinedOutput(); err != nil {
		t.Skipf("Docker daemon is not available: %v: %s", err, strings.TrimSpace(string(output)))
	}
	const image = "debian:trixie-slim"
	if output, err := exec.Command("docker", "image", "inspect", image).CombinedOutput(); err != nil {
		t.Skipf("Docker image %s is not available locally: %v: %s", image, err, strings.TrimSpace(string(output)))
	}
	beforeOutput, err := exec.Command("docker", "ps", "--format", "{{.Names}}", "--filter", "name=sigma-agent-session-").CombinedOutput()
	if err != nil {
		t.Fatalf("list existing Sigma session containers: %v: %s", err, strings.TrimSpace(string(beforeOutput)))
	}
	before := make(map[string]bool)
	for _, name := range strings.Fields(string(beforeOutput)) {
		before[name] = true
	}
	session := &commandContainerSession{root: t.TempDir(), allowNetwork: false}
	t.Cleanup(func() { _ = session.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, _, runErr := session.run(ctx, image, "sleep 60", session.root)
		done <- runErr
	}()
	var container string
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		output, listErr := exec.Command("docker", "ps", "--format", "{{.Names}}", "--filter", "name=sigma-agent-session-").CombinedOutput()
		if listErr != nil {
			cancel()
			t.Fatalf("poll session container: %v: %s", listErr, strings.TrimSpace(string(output)))
		}
		for _, name := range strings.Fields(string(output)) {
			if !before[name] {
				container = name
				break
			}
		}
		if container != "" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if container == "" {
		cancel()
		t.Fatal("session container did not start in time")
	}
	cancel()
	select {
	case runErr := <-done:
		if runErr == nil {
			t.Fatal("cancelled Docker exec returned no error")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("cancelled session command did not return")
	}
	if _, err := exec.Command("docker", "inspect", container).CombinedOutput(); err == nil {
		t.Fatalf("cancelled session container %q remained", container)
	}
}

func TestDockerBackgroundContainerUsesSameRestrictionsAndCleansUp(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is not available")
	}
	if output, err := exec.Command("docker", "info", "--format", "{{.ServerVersion}}").CombinedOutput(); err != nil {
		t.Skipf("Docker daemon is not available: %v: %s", err, strings.TrimSpace(string(output)))
	}
	const image = "debian:trixie-slim"
	if output, err := exec.Command("docker", "image", "inspect", image).CombinedOutput(); err != nil {
		t.Skipf("Docker image %s is not available locally: %v: %s", image, err, strings.TrimSpace(string(output)))
	}
	root := t.TempDir()
	stateDir := t.TempDir()
	manager, err := NewBackgroundCommandManager(root, image, stateDir, "", false)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	job, err := manager.StartWithImage(ctx, image, "sleep 1; printf background-ok > /workspace/background-result.txt", root, "inspect")
	if err != nil {
		t.Fatalf("start background container: %v", err)
	}
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "--force", job.Container).Run()
		_ = removeIsolatedTestNetwork(job.Network)
	})
	var hostConfig struct {
		ReadonlyRootfs bool   `json:"ReadonlyRootfs"`
		Memory         int64  `json:"Memory"`
		NanoCpus       int64  `json:"NanoCpus"`
		PidsLimit      int64  `json:"PidsLimit"`
		NetworkMode    string `json:"NetworkMode"`
	}
	inspect, err := exec.Command("docker", "inspect", "--format", "{{json .HostConfig}}", job.Container).CombinedOutput()
	if err != nil {
		t.Fatalf("inspect background container: %v: %s", err, strings.TrimSpace(string(inspect)))
	}
	if err := json.Unmarshal(inspect, &hostConfig); err != nil {
		t.Fatalf("decode background HostConfig %s: %v", inspect, err)
	}
	if !hostConfig.ReadonlyRootfs || hostConfig.Memory != 2<<30 || hostConfig.NanoCpus != 2_000_000_000 || hostConfig.PidsLimit != 128 || hostConfig.NetworkMode != "none" {
		t.Errorf("background container restrictions = %#v, want same limits as foreground", hostConfig)
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		job, err = manager.Status(ctx, job.ID)
		if err != nil {
			t.Fatalf("poll background task: %v", err)
		}
		if !isActiveBackgroundStatus(job.Status) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("background task did not finish; last state: %#v", job)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if job.Status != "passed" || job.ExitCode != 0 {
		t.Fatalf("background task result = status %q exit %d: %s", job.Status, job.ExitCode, job.Output)
	}
	if contents, err := os.ReadFile(filepath.Join(root, "background-result.txt")); err != nil || string(contents) != "background-ok" {
		t.Fatalf("background workspace write = %q, %v; want background-ok", contents, err)
	}
	if _, err := exec.Command("docker", "inspect", job.Container).CombinedOutput(); err == nil {
		t.Fatalf("completed background container %q was not removed", job.Container)
	}
	stoppable, err := manager.StartWithImage(ctx, image, "sleep 60", root, "inspect")
	if err != nil {
		t.Fatalf("start stoppable background task: %v", err)
	}
	t.Cleanup(func() {
		_ = exec.Command("docker", "rm", "--force", stoppable.Container).Run()
		_ = removeIsolatedTestNetwork(stoppable.Network)
	})
	stopped, err := manager.Stop(ctx, stoppable.ID)
	if err != nil {
		t.Fatalf("stop background task: %v", err)
	}
	if stopped.Status != "stopped" {
		t.Fatalf("stopped task status = %q, want stopped", stopped.Status)
	}
	if _, err := exec.Command("docker", "inspect", stoppable.Container).CombinedOutput(); err == nil {
		t.Fatalf("stopped background container %q was not removed", stoppable.Container)
	}
}

func containsTestString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// run_command must advertise the container network mode so the model knows
// whether it can download dependencies.
func TestRunCommandDescribesTheConfiguredNetworkMode(t *testing.T) {
	off := workspace{root: t.TempDir(), allowNetwork: false}.runCommandTool("debian:trixie-slim")
	if !strings.Contains(off.Definition.Description, "no external network") {
		t.Fatalf("run_command description does not report the disabled network: %q", off.Definition.Description)
	}
	on := workspace{root: t.TempDir(), allowNetwork: true}.runCommandTool("golang:1.26")
	if !strings.Contains(on.Definition.Description, "internet access is enabled") {
		t.Fatalf("run_command description does not report the enabled network: %q", on.Definition.Description)
	}
}

func TestWorkspaceCommandFilesystemScope(t *testing.T) {
	tests := []struct {
		name    string
		command string
		wantErr bool
	}{
		{name: "benchmark external enum source", command: `grep -n "__str__" /opt/miniconda3/envs/testbed/lib/python3.6/enum.py`, wantErr: true},
		{name: "other absolute system path", command: `sed -n 1,20p /etc/passwd`, wantErr: true},
		{name: "temporary directory", command: `cat /tmp/probe.py`, wantErr: true},
		{name: "parent traversal", command: `cat ../../outside.txt`, wantErr: true},
		{name: "home expansion", command: `cat ~/outside.txt`, wantErr: true},
		{name: "environment based path", command: `cat "$CONDA_PREFIX/lib/python3.6/enum.py"`, wantErr: true},
		{name: "workspace absolute path", command: `cat /workspace/django/db/models/query.py`},
		{name: "workspace relative path", command: `python tests/runtests.py bulk_create --parallel=1`},
		{name: "safe null redirection", command: `python tests/runtests.py bulk_create 2>/dev/null`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateCommandFilesystemScope(test.command, "workspace")
			if test.wantErr && err == nil {
				t.Fatalf("validateCommandFilesystemScope(%q) = nil, want error", test.command)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("validateCommandFilesystemScope(%q) error = %v", test.command, err)
			}
		})
	}
}
