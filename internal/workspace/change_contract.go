package workspace

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/sdaAlbert/sigma-agent/internal/agent"
)

type changeContractGate struct {
	mu               sync.Mutex
	readPaths        map[string]struct{}
	readVersions     map[string]string
	contractRecorded bool
}

func newChangeContractGate() *changeContractGate {
	return &changeContractGate{
		readPaths:    make(map[string]struct{}),
		readVersions: make(map[string]string),
	}
}

func (g *changeContractGate) markRead(path, version string) {
	if g == nil {
		return
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	g.mu.Lock()
	g.readPaths[clean] = struct{}{}
	g.readVersions[clean] = version
	g.mu.Unlock()
}

func (g *changeContractGate) readVersion(path string) (string, bool) {
	if g == nil {
		return "", false
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	g.mu.Lock()
	defer g.mu.Unlock()
	version, ok := g.readVersions[clean]
	return version, ok
}

func (g *changeContractGate) clearReadVersion(path string) {
	if g == nil {
		return
	}
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
	g.mu.Lock()
	delete(g.readVersions, clean)
	g.mu.Unlock()
}

func (w workspace) requireRecordedChangeContract() error {
	if !w.requireChangeContract {
		return nil
	}
	if w.changeGate == nil {
		return errors.New("change-contract gate is unavailable")
	}
	w.changeGate.mu.Lock()
	defer w.changeGate.mu.Unlock()
	if !w.changeGate.contractRecorded {
		return errors.New("benchmark write gate: call record_change_contract after reading the issue, relevant implementation, and tests; include the exact expected output form, then retry the write")
	}
	return nil
}

func (w workspace) recordChangeContractTool() agent.Tool {
	description := "Optional planning aid for multi-layer or risky changes. Record observable behavior, intended scope, affected callers, execution path, evidence paths already read with read_file, positive/negative acceptance checks, and any exact output syntax from the issue or tests."
	if w.requireChangeContract {
		description = "Required benchmark checkpoint before any source write. Read the issue, relevant implementation, and tests; record the behavior, scope, callers, execution path, evidence paths, positive/negative checks, and exact expected output syntax verbatim."
	}
	schema := `{"type":"object","properties":{"behavior_and_trigger":{"type":"string","minLength":8,"maxLength":2000},"scope_and_reason":{"type":"string","minLength":8,"maxLength":2000},"existing_contract_and_callers":{"type":"string","minLength":8,"maxLength":3000},"execution_path_and_short_circuits":{"type":"string","minLength":20,"maxLength":4000},"evidence_paths":{"type":"array","minItems":2,"maxItems":8,"items":{"type":"string","minLength":1,"maxLength":1024}},"positive_acceptance":{"type":"string","minLength":4,"maxLength":1500},"negative_acceptance":{"type":"string","minLength":4,"maxLength":1500},"exact_acceptance_examples":{"type":"array","minItems":1,"maxItems":8,"items":{"type":"string","minLength":1,"maxLength":300}}},"required":["behavior_and_trigger","scope_and_reason","existing_contract_and_callers","execution_path_and_short_circuits","evidence_paths","positive_acceptance","negative_acceptance"],"additionalProperties":false}`
	return tool("record_change_contract", description, schema, "sequential", func(ctx context.Context, raw json.RawMessage) (string, error) {
		var contract struct {
			BehaviorAndTrigger         string   `json:"behavior_and_trigger"`
			ScopeAndReason             string   `json:"scope_and_reason"`
			ExistingContractAndCallers string   `json:"existing_contract_and_callers"`
			ExecutionPathAndShortCuts  string   `json:"execution_path_and_short_circuits"`
			EvidencePaths              []string `json:"evidence_paths"`
			PositiveAcceptance         string   `json:"positive_acceptance"`
			NegativeAcceptance         string   `json:"negative_acceptance"`
			ExactAcceptanceExamples    []string `json:"exact_acceptance_examples"`
		}
		if err := decodeArgs(raw, &contract); err != nil {
			return "", err
		}
		contract.BehaviorAndTrigger = strings.TrimSpace(contract.BehaviorAndTrigger)
		contract.ScopeAndReason = strings.TrimSpace(contract.ScopeAndReason)
		contract.ExistingContractAndCallers = strings.TrimSpace(contract.ExistingContractAndCallers)
		contract.ExecutionPathAndShortCuts = strings.TrimSpace(contract.ExecutionPathAndShortCuts)
		contract.PositiveAcceptance = strings.TrimSpace(contract.PositiveAcceptance)
		contract.NegativeAcceptance = strings.TrimSpace(contract.NegativeAcceptance)
		if len(contract.BehaviorAndTrigger) < 8 || len(contract.BehaviorAndTrigger) > 2000 ||
			len(contract.ScopeAndReason) < 8 || len(contract.ScopeAndReason) > 2000 ||
			len(contract.ExistingContractAndCallers) < 8 || len(contract.ExistingContractAndCallers) > 3000 ||
			len(contract.ExecutionPathAndShortCuts) < 20 || len(contract.ExecutionPathAndShortCuts) > 4000 ||
			len(contract.PositiveAcceptance) < 4 || len(contract.PositiveAcceptance) > 1500 ||
			len(contract.NegativeAcceptance) < 4 || len(contract.NegativeAcceptance) > 1500 {
			return "", errors.New("change contract fields are incomplete or exceed their size limits")
		}
		if len(contract.EvidencePaths) < 2 || len(contract.EvidencePaths) > 8 {
			return "", errors.New("provide two to eight distinct evidence paths that were read with read_file")
		}
		if w.requireChangeContract && len(contract.ExactAcceptanceExamples) == 0 {
			return "", errors.New("benchmark change contract requires at least one exact acceptance example copied from the issue or existing tests")
		}
		if w.changeGate == nil {
			return "", errors.New("change-contract recorder is unavailable")
		}

		seen := make(map[string]struct{}, len(contract.EvidencePaths))
		w.changeGate.mu.Lock()
		defer w.changeGate.mu.Unlock()
		for _, path := range contract.EvidencePaths {
			clean, err := normalizeContractEvidencePath(path)
			if err != nil {
				return "", err
			}
			if _, ok := seen[clean]; ok {
				return "", fmt.Errorf("evidence path %q is duplicated", path)
			}
			seen[clean] = struct{}{}
			if _, ok := w.changeGate.readPaths[clean]; !ok {
				return "", fmt.Errorf("evidence path %q must be successfully read with read_file before recording the contract", path)
			}
		}
		if w.requireChangeContract {
			w.changeGate.contractRecorded = true
		}
		return fmt.Sprintf("Change plan recorded.\nBehavior and trigger: %s\nScope and reason: %s\nExisting contract and callers: %s\nExecution path and short-circuit points: %s\nPositive acceptance: %s\nNegative acceptance: %s\nExact acceptance examples: %s", contract.BehaviorAndTrigger, contract.ScopeAndReason, contract.ExistingContractAndCallers, contract.ExecutionPathAndShortCuts, contract.PositiveAcceptance, contract.NegativeAcceptance, strings.Join(contract.ExactAcceptanceExamples, "; ")), nil
	})
}

func normalizeContractEvidencePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("evidence paths must not be empty")
	}
	clean := filepath.Clean(filepath.FromSlash(path))
	if filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("evidence path %q must be workspace-relative", path)
	}
	return filepath.ToSlash(clean), nil
}
