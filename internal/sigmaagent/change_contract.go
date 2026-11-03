package sigmaagent

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
	mu           sync.Mutex
	readPaths    map[string]struct{}
	readVersions map[string]string
	contractTurn int
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

func (w workspace) recordChangeContractTool() agent.Tool {
	description := "Before changing source files, record the observable behavior and trigger, intended scope and why, the existing interface and affected callers, the ordered execution path from the outer entry point through dispatch/wrappers to the behavior and response, and where a layer can short-circuit later layers. For request-routing issues, include route/method matching and whether an explicit handler or fallback runs. Add at least one positive and one negative acceptance check, plus at least two evidence paths already read with read_file. Make checks concrete enough to guide implementation and name a focused test when one exists. Runtime blocks file writes in the same assistant turn; wait for the acknowledgement in the next turn before editing. If new evidence changes the path, scope, or interface, record an updated contract before the next edit."
	schema := `{"type":"object","properties":{"behavior_and_trigger":{"type":"string","minLength":8,"maxLength":2000},"scope_and_reason":{"type":"string","minLength":8,"maxLength":2000},"existing_contract_and_callers":{"type":"string","minLength":8,"maxLength":3000},"execution_path_and_short_circuits":{"type":"string","minLength":20,"maxLength":4000},"evidence_paths":{"type":"array","minItems":2,"maxItems":8,"items":{"type":"string","minLength":1,"maxLength":1024}},"positive_acceptance":{"type":"string","minLength":4,"maxLength":1500},"negative_acceptance":{"type":"string","minLength":4,"maxLength":1500}},"required":["behavior_and_trigger","scope_and_reason","existing_contract_and_callers","execution_path_and_short_circuits","evidence_paths","positive_acceptance","negative_acceptance"],"additionalProperties":false}`
	return tool("record_change_contract", description, schema, "sequential", func(ctx context.Context, raw json.RawMessage) (string, error) {
		var contract struct {
			BehaviorAndTrigger         string   `json:"behavior_and_trigger"`
			ScopeAndReason             string   `json:"scope_and_reason"`
			ExistingContractAndCallers string   `json:"existing_contract_and_callers"`
			ExecutionPathAndShortCuts  string   `json:"execution_path_and_short_circuits"`
			EvidencePaths              []string `json:"evidence_paths"`
			PositiveAcceptance         string   `json:"positive_acceptance"`
			NegativeAcceptance         string   `json:"negative_acceptance"`
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
		turn, ok := agent.ToolTurn(ctx)
		if !ok {
			return "", errors.New("tool turn metadata is unavailable; cannot record a change contract")
		}
		if w.changeGate == nil {
			return "", errors.New("change-contract gate is unavailable")
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
		w.changeGate.contractTurn = turn
		return fmt.Sprintf("Change contract recorded on assistant turn %d. Source writes are unlocked only on a later assistant turn.\nBehavior and trigger: %s\nScope and reason: %s\nExisting contract and callers: %s\nExecution path and short-circuit points: %s\nPositive acceptance: %s\nNegative acceptance: %s", turn, contract.BehaviorAndTrigger, contract.ScopeAndReason, contract.ExistingContractAndCallers, contract.ExecutionPathAndShortCuts, contract.PositiveAcceptance, contract.NegativeAcceptance), nil
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

func (w workspace) requireChangeContract(ctx context.Context) error {
	if w.changeGate == nil {
		return nil
	}
	turn, ok := agent.ToolTurn(ctx)
	if !ok {
		return errors.New("tool turn metadata is unavailable; source writes require a recorded change contract")
	}
	w.changeGate.mu.Lock()
	contractTurn := w.changeGate.contractTurn
	w.changeGate.mu.Unlock()
	if contractTurn == 0 {
		return errors.New("source write blocked: first inspect relevant source, callers, and tests with read_file, then call record_change_contract")
	}
	if turn <= contractTurn {
		return errors.New("source write blocked: record_change_contract must succeed in an earlier assistant turn; review its acknowledgement before editing")
	}
	return nil
}
