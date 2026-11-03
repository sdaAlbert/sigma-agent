package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/sdaAlbert/sigma-agent/internal/sigmaagent"
)

type retrievalCase struct {
	InstanceID string `json:"instance_id"`
	Repo       string `json:"repo"`
	QueryFile  string `json:"query_file"`
}

type retrievalResult struct {
	InstanceID string `json:"instance_id"`
	Query      string `json:"query"`
	RawResult  string `json:"raw_result"`
}

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: memory_preflight.go <cases.json> <results.json>")
		os.Exit(2)
	}
	input, err := os.ReadFile(os.Args[1])
	if err != nil {
		fatal(err)
	}
	var cases []retrievalCase
	if err := json.Unmarshal(input, &cases); err != nil {
		fatal(err)
	}
	results := make([]retrievalResult, 0, len(cases))
	for _, item := range cases {
		queryBytes, err := os.ReadFile(item.QueryFile)
		if err != nil {
			fatal(err)
		}
		query := string(queryBytes)
		if len(query) > 8192 {
			query = query[:8192]
		}
		tools, err := sigmaagent.BuiltinTools(item.Repo, "golang:1.26", 8192, nil, nil)
		if err != nil {
			fatal(err)
		}
		var rawResult string
		for _, candidate := range tools {
			if candidate.Definition.Name != "search_memory" {
				continue
			}
			args, err := json.Marshal(map[string]any{"query": query, "top_k": 1})
			if err != nil {
				fatal(err)
			}
			rawResult, err = candidate.Handle(context.Background(), args)
			if err != nil {
				fatal(err)
			}
			break
		}
		results = append(results, retrievalResult{InstanceID: item.InstanceID, Query: query, RawResult: rawResult})
	}
	output, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		fatal(err)
	}
	if err := os.WriteFile(os.Args[2], append(output, '\n'), 0o600); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
