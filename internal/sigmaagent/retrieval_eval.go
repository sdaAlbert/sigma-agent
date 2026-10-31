package sigmaagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type RetrievalCase struct {
	Dataset       string   `json:"dataset,omitempty"`
	ID            string   `json:"id"`
	Repo          string   `json:"repo"`
	BaseCommit    string   `json:"base_commit"`
	Query         string   `json:"query"`
	RelevantPaths []string `json:"relevant_paths"`
}

type RetrievalMetrics struct {
	Cases   int     `json:"cases"`
	RecallK float64 `json:"recall_at_k"`
	MRR     float64 `json:"mrr"`
}

type RetrievalEvaluation struct {
	Dataset string                `json:"dataset,omitempty"`
	K       int                   `json:"k"`
	BM25    RetrievalMetrics      `json:"bm25"`
	Hybrid  *RetrievalMetrics     `json:"hybrid_bm25_vector_rrf,omitempty"`
	Results []RetrievalCaseResult `json:"results"`
}

type RetrievalCaseResult struct {
	ID            string   `json:"id"`
	BaseCommit    string   `json:"base_commit,omitempty"`
	RelevantPaths []string `json:"relevant_paths"`
	BM25Paths     []string `json:"bm25_paths"`
	BM25Recall    float64  `json:"bm25_recall_at_k"`
	BM25MRR       float64  `json:"bm25_mrr"`
	HybridPaths   []string `json:"hybrid_paths,omitempty"`
	HybridRecall  float64  `json:"hybrid_recall_at_k,omitempty"`
	HybridMRR     float64  `json:"hybrid_mrr,omitempty"`
}

// EvaluateRetrieval evaluates path retrieval against the repository snapshot
// specified by each case. Vector retrieval is included when configured.
func EvaluateRetrieval(root string, cases []RetrievalCase, k int) (RetrievalEvaluation, error) {
	if len(cases) == 0 {
		return RetrievalEvaluation{}, errors.New("retrieval evaluation dataset is empty")
	}
	if k < 1 {
		k = 5
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return RetrievalEvaluation{}, err
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return RetrievalEvaluation{}, err
	}
	repoRoot := filepath.Clean(abs)
	vector := newVectorConfig()
	var bm25Sum, bm25MRR, hybridSum, hybridMRR float64
	ctx := context.Background()
	results := make([]RetrievalCaseResult, 0, len(cases))
	for _, item := range cases {
		if strings.TrimSpace(item.Query) == "" || len(item.RelevantPaths) == 0 {
			return RetrievalEvaluation{}, fmt.Errorf("case %q needs a query and relevant_paths", item.ID)
		}
		if item.Repo != "" {
			if err := verifyRetrievalRepository(repoRoot, item.Repo); err != nil {
				return RetrievalEvaluation{}, fmt.Errorf("case %q: %w", item.ID, err)
			}
		}
		caseRoot, cleanup, err := retrievalCaseWorkspace(ctx, repoRoot, item.BaseCommit)
		if err != nil {
			return RetrievalEvaluation{}, fmt.Errorf("prepare case %q: %w", item.ID, err)
		}
		caseResult, evalErr := evaluateRetrievalCase(ctx, caseRoot, item, k, vector)
		cleanup()
		if evalErr != nil {
			return RetrievalEvaluation{}, fmt.Errorf("evaluate case %q: %w", item.ID, evalErr)
		}
		bm25Sum += caseResult.BM25Recall
		bm25MRR += caseResult.BM25MRR
		if vector != nil {
			hybridSum += caseResult.HybridRecall
			hybridMRR += caseResult.HybridMRR
		}
		results = append(results, caseResult)
	}
	denom := float64(len(cases))
	evaluation := RetrievalEvaluation{Dataset: cases[0].Dataset, K: k, Results: results,
		BM25: RetrievalMetrics{Cases: len(cases), RecallK: bm25Sum / denom, MRR: bm25MRR / denom}}
	if vector != nil {
		evaluation.Hybrid = &RetrievalMetrics{Cases: len(cases), RecallK: hybridSum / denom, MRR: hybridMRR / denom}
	}
	return evaluation, nil
}

// EvaluateRetrievalDataset evaluates a multi-repository dataset. Repositories
// are cached under repoCacheRoot and checked out at each case's pinned base commit.
func EvaluateRetrievalDataset(repoCacheRoot string, cases []RetrievalCase, k int) (RetrievalEvaluation, error) {
	if len(cases) == 0 {
		return RetrievalEvaluation{}, errors.New("retrieval evaluation dataset is empty")
	}
	groups := make(map[string][]RetrievalCase)
	var repos []string
	for _, item := range cases {
		if strings.TrimSpace(item.Repo) == "" {
			return RetrievalEvaluation{}, fmt.Errorf("case %q has no repo", item.ID)
		}
		if _, ok := groups[item.Repo]; !ok {
			repos = append(repos, item.Repo)
		}
		groups[item.Repo] = append(groups[item.Repo], item)
	}
	sort.Strings(repos)
	cacheRoot, err := filepath.Abs(repoCacheRoot)
	if err != nil {
		return RetrievalEvaluation{}, err
	}
	if err := os.MkdirAll(cacheRoot, 0o755); err != nil {
		return RetrievalEvaluation{}, err
	}
	combined := RetrievalEvaluation{K: k}
	var bm25Recall, bm25MRR, hybridRecall, hybridMRR float64
	for _, repo := range repos {
		root, err := cachedBenchmarkRepository(cacheRoot, repo)
		if err != nil {
			return RetrievalEvaluation{}, fmt.Errorf("prepare repository %s: %w", repo, err)
		}
		fmt.Fprintf(os.Stderr, "Evaluating %s (%d cases)\n", repo, len(groups[repo]))
		part, err := EvaluateRetrieval(root, groups[repo], k)
		if err != nil {
			return RetrievalEvaluation{}, fmt.Errorf("evaluate repository %s: %w", repo, err)
		}
		if combined.Dataset == "" {
			combined.Dataset = groups[repo][0].Dataset
		}
		combined.Results = append(combined.Results, part.Results...)
		bm25Recall += part.BM25.RecallK * float64(part.BM25.Cases)
		bm25MRR += part.BM25.MRR * float64(part.BM25.Cases)
		if part.Hybrid != nil {
			if combined.Hybrid == nil {
				combined.Hybrid = &RetrievalMetrics{}
			}
			hybridRecall += part.Hybrid.RecallK * float64(part.Hybrid.Cases)
			hybridMRR += part.Hybrid.MRR * float64(part.Hybrid.Cases)
		}
	}
	combined.BM25.Cases = len(cases)
	combined.BM25.RecallK = bm25Recall / float64(len(cases))
	combined.BM25.MRR = bm25MRR / float64(len(cases))
	if combined.Hybrid != nil {
		combined.Hybrid.Cases = len(cases)
		combined.Hybrid.RecallK = hybridRecall / float64(len(cases))
		combined.Hybrid.MRR = hybridMRR / float64(len(cases))
	}
	return combined, nil
}

func cachedBenchmarkRepository(cacheRoot, name string) (string, error) {
	parts := strings.Split(name, "/")
	if len(parts) != 2 || !validRepositoryNamePart(parts[0]) || !validRepositoryNamePart(parts[1]) {
		return "", fmt.Errorf("invalid repository name %q; expected owner/repository", name)
	}
	root := filepath.Join(cacheRoot, parts[0], parts[1])
	if _, err := os.Stat(filepath.Join(root, ".git")); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(filepath.Dir(root), 0o755); err != nil {
			return "", err
		}
		command := exec.Command("git", "clone", "--filter=blob:none", "--no-checkout", "https://github.com/"+name+".git", root)
		if output, err := command.CombinedOutput(); err != nil {
			return "", fmt.Errorf("git clone: %w: %s", err, strings.TrimSpace(string(output)))
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	if err := verifyRetrievalRepository(root, name); err != nil {
		return "", err
	}
	return root, nil
}

func validRepositoryNamePart(part string) bool {
	if part == "" || part == "." || part == ".." {
		return false
	}
	for _, r := range part {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

func evaluateRetrievalCase(ctx context.Context, root string, item RetrievalCase, k int, vector *vectorConfig) (RetrievalCaseResult, error) {
	w := workspace{root: root}
	files, err := w.walk(w.root)
	if err != nil {
		return RetrievalCaseResult{}, err
	}
	chunks := collectChunks(w, files)
	if len(chunks) == 0 {
		return RetrievalCaseResult{}, errors.New("repository has no indexable chunks")
	}
	lexical := rankChunks(chunks, item.Query)
	result := RetrievalCaseResult{ID: item.ID, BaseCommit: item.BaseCommit, RelevantPaths: item.RelevantPaths,
		BM25Paths: topPaths(lexical, k)}
	result.BM25Recall, result.BM25MRR = measurePaths(lexical, item.RelevantPaths, k)
	if vector == nil {
		return result, nil
	}
	if _, err := vector.index(ctx, root); err != nil {
		return RetrievalCaseResult{}, fmt.Errorf("index vector snapshot: %w", err)
	}
	hybrid, err := w.retrieveHybrid(ctx, item.Query, k, vector)
	if err != nil {
		return RetrievalCaseResult{}, fmt.Errorf("hybrid query: %w", err)
	}
	result.HybridPaths = topPaths(hybrid, k)
	result.HybridRecall, result.HybridMRR = measurePaths(hybrid, item.RelevantPaths, k)
	return result, nil
}

func topPaths(chunks []scoredChunk, k int) []string {
	paths := make([]string, 0, len(chunks))
	seen := make(map[string]bool)
	for _, chunk := range chunks {
		if !seen[chunk.path] {
			seen[chunk.path] = true
			paths = append(paths, chunk.path)
			if len(paths) == k {
				break
			}
		}
	}
	return paths
}

func verifyRetrievalRepository(root, expected string) error {
	command := exec.Command("git", "-C", root, "remote", "get-url", "origin")
	output, err := command.Output()
	if err != nil {
		return fmt.Errorf("cannot identify repository origin; expected a checkout of %s", expected)
	}
	remote := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(string(output)), ".git"))
	want := strings.ToLower(strings.TrimSuffix(expected, ".git"))
	if !strings.Contains(remote, want) {
		return fmt.Errorf("--repo origin %q does not match dataset repository %q", strings.TrimSpace(string(output)), expected)
	}
	return nil
}

func retrievalCaseWorkspace(ctx context.Context, repoRoot, commit string) (string, func(), error) {
	if strings.TrimSpace(commit) == "" {
		return repoRoot, func() {}, nil
	}
	runGit := func(args ...string) error {
		command := exec.CommandContext(ctx, "git", append([]string{"-C", repoRoot}, args...)...)
		if output, err := command.CombinedOutput(); err != nil {
			return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(output)))
		}
		return nil
	}
	if err := runGit("cat-file", "-e", commit+"^{commit}"); err != nil {
		if fetchErr := runGit("fetch", "--quiet", "origin", commit); fetchErr != nil {
			return "", nil, fmt.Errorf("base commit %s is unavailable and could not be fetched: %w", commit, fetchErr)
		}
		if err := runGit("cat-file", "-e", commit+"^{commit}"); err != nil {
			return "", nil, fmt.Errorf("base commit %s is unavailable: %w", commit, err)
		}
	}
	worktree, err := os.MkdirTemp("", "sigmaagent-retrieval-")
	if err != nil {
		return "", nil, err
	}
	if err := os.Remove(worktree); err != nil {
		return "", nil, err
	}
	if err := runGit("worktree", "add", "--quiet", "--detach", worktree, commit); err != nil {
		return "", nil, err
	}
	cleanup := func() {
		_ = runGit("worktree", "remove", "--force", worktree)
		_ = runGit("worktree", "prune")
	}
	return worktree, cleanup, nil
}

func collectChunks(w workspace, files []string) []scoredChunk {
	var chunks []scoredChunk
	for _, relative := range files {
		data, err := readBounded(filepath.Join(w.root, filepath.FromSlash(relative)), maxFileBytes)
		if err != nil {
			continue
		}
		lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
		for start := 0; start < len(lines); start += 68 {
			end := start + 80
			if end > len(lines) {
				end = len(lines)
			}
			chunks = append(chunks, scoredChunk{path: relative, start: start + 1, end: end, content: strings.Join(lines[start:end], "\n")})
			if end == len(lines) {
				break
			}
		}
	}
	return chunks
}

func rankChunks(chunks []scoredChunk, query string) []scoredChunk {
	queryTerms := tokenize(query)
	termFreqs := make([]map[string]int, len(chunks))
	lengths := make([]int, len(chunks))
	documentFreq := make(map[string]int)
	totalLength := 0
	for i, chunk := range chunks {
		terms := tokenize(chunk.content)
		freq := make(map[string]int)
		for _, term := range terms {
			freq[term]++
		}
		termFreqs[i], lengths[i] = freq, len(terms)
		totalLength += len(terms)
		for term := range freq {
			documentFreq[term]++
		}
	}
	averageLength := float64(totalLength) / math.Max(1, float64(len(chunks)))
	const k1, b = 1.2, 0.75
	ranked := make([]scoredChunk, 0, len(chunks))
	for i, chunk := range chunks {
		unique := make(map[string]bool)
		for _, term := range queryTerms {
			if unique[term] {
				continue
			}
			unique[term] = true
			tf := float64(termFreqs[i][term])
			if tf == 0 {
				continue
			}
			df := float64(documentFreq[term])
			idf := math.Log(1 + (float64(len(chunks))-df+0.5)/(df+0.5))
			denom := tf + k1*(1-b+b*float64(lengths[i])/math.Max(1, averageLength))
			chunk.score += idf * tf * (k1 + 1) / denom
			if strings.Contains(strings.ToLower(chunk.path), term) {
				chunk.score += 0.35 * idf
			}
		}
		if chunk.score > 0 {
			ranked = append(ranked, chunk)
		}
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		if ranked[i].score == ranked[j].score {
			if ranked[i].path == ranked[j].path {
				return ranked[i].start < ranked[j].start
			}
			return ranked[i].path < ranked[j].path
		}
		return ranked[i].score > ranked[j].score
	})
	return ranked
}

func measurePaths(results []scoredChunk, relevant []string, k int) (float64, float64) {
	gold := make(map[string]bool, len(relevant))
	for _, path := range relevant {
		gold[filepath.ToSlash(path)] = true
	}
	seen := make(map[string]bool)
	hits := 0
	first := 0
	for _, result := range results {
		if len(seen) >= k {
			break
		}
		if seen[result.path] {
			continue
		}
		seen[result.path] = true
		if gold[result.path] {
			hits++
			if first == 0 {
				first = len(seen)
			}
		}
	}
	recall := float64(hits) / float64(len(gold))
	if first == 0 {
		return recall, 0
	}
	return recall, 1 / float64(first)
}

func LoadRetrievalCases(path string) ([]RetrievalCase, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cases []RetrievalCase
	if err := json.Unmarshal(data, &cases); err != nil {
		return nil, err
	}
	return cases, nil
}
