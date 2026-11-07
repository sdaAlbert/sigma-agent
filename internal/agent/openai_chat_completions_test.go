package agent

import (
	"strings"
	"testing"
)

func TestDecodeOpenAIChatStreamCacheUsage(t *testing.T) {
	tests := []struct {
		name     string
		usage    string
		wantHit  int
		wantMiss int
	}{
		{
			name:     "deepseek explicit cache counters",
			usage:    `"usage":{"prompt_tokens":100,"completion_tokens":3,"prompt_cache_hit_tokens":72,"prompt_cache_miss_tokens":28}`,
			wantHit:  72,
			wantMiss: 28,
		},
		{
			name:     "cached token details fallback",
			usage:    `"usage":{"prompt_tokens":100,"completion_tokens":3,"prompt_tokens_details":{"cached_tokens":64}}`,
			wantHit:  64,
			wantMiss: 36,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stream := `data: {"choices":[{"delta":{},"finish_reason":"stop"}],` + test.usage + `}` + "\n\n" + "data: [DONE]\n\n"
			response, err := decodeOpenAIChatStream(strings.NewReader(stream), nil)
			if err != nil {
				t.Fatal(err)
			}
			if response.InputTokens != 100 || response.OutputTokens != 3 {
				t.Fatalf("token totals = input %d, output %d; want 100, 3", response.InputTokens, response.OutputTokens)
			}
			if response.CacheReadInputTokens != test.wantHit || response.CacheMissInputTokens != test.wantMiss {
				t.Fatalf("cache tokens = hit %d, miss %d; want %d, %d", response.CacheReadInputTokens, response.CacheMissInputTokens, test.wantHit, test.wantMiss)
			}
		})
	}
}
