package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/sdaAlbert/sigma-agent/internal/agent"
)

// appendAuthorizationAuditEvent persists a decision to both the current run
// trace and the session-scoped log before the caller permits tool execution.
func appendAuthorizationAuditEvent(event agent.Event, sessionID string, sessionLog, runTrace *os.File, mu *sync.Mutex) (agent.Event, error) {
	if sessionLog == nil || runTrace == nil || mu == nil {
		return event, fmt.Errorf("authorization audit log is unavailable")
	}
	event.At = time.Now().UTC()
	event.Session = sessionID
	mu.Lock()
	defer mu.Unlock()
	if err := json.NewEncoder(sessionLog).Encode(event); err != nil {
		return event, fmt.Errorf("write session authorization event: %w", err)
	}
	if err := sessionLog.Sync(); err != nil {
		return event, fmt.Errorf("flush session authorization event: %w", err)
	}
	if err := json.NewEncoder(runTrace).Encode(event); err != nil {
		return event, fmt.Errorf("write authorization event: %w", err)
	}
	if err := runTrace.Sync(); err != nil {
		return event, fmt.Errorf("flush authorization event: %w", err)
	}
	return event, nil
}
