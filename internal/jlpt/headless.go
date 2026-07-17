package jlpt

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// HeadlessConfig configures Grok Build CLI classification.
type HeadlessConfig struct {
	// Bin is the CLI binary (default "grok").
	Bin string
	// Model is passed as -m (default composer-2.5).
	Model string
	// Timeout per classify call (default 12s).
	Timeout time.Duration
	// ExtraArgs appended after fixed flags (tests).
	ExtraArgs []string
	// Runner overrides exec (tests). If nil, uses exec.CommandContext.
	Runner func(ctx context.Context, name string, args ...string) ([]byte, error)
}

// HeadlessClassifier shells out to Grok Build headless with a strict JSON prompt.
type HeadlessClassifier struct {
	cfg HeadlessConfig
	mu  sync.Mutex // serialize CLI calls
}

// NewHeadless builds a HeadlessClassifier with defaults filled.
func NewHeadless(cfg HeadlessConfig) *HeadlessClassifier {
	if strings.TrimSpace(cfg.Bin) == "" {
		cfg.Bin = "grok"
	}
	if strings.TrimSpace(cfg.Model) == "" {
		cfg.Model = "composer-2.5"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 12 * time.Second
	}
	return &HeadlessClassifier{cfg: cfg}
}

// BuildArgs returns the CLI argv for tests (without binary name).
func (h *HeadlessClassifier) BuildArgs(prompt string) []string {
	args := []string{
		"-p", prompt,
		"-m", h.cfg.Model,
		"--output-format", "json",
		"--max-turns", "1",
		"--disallowed-tools", "Agent,web_search,web_fetch,run_terminal_cmd,search_replace,write",
	}
	args = append(args, h.cfg.ExtraArgs...)
	return args
}

// ClassifyPrompt is the strict prompt body for one Word.
func ClassifyPrompt(lemma, reading string) string {
	return fmt.Sprintf(
		`You classify Japanese vocabulary for JLPT-ish difficulty.
Lemma: %s
Reading: %s
Reply with ONLY JSON: {"level":"n5"|"n4"|"n3"|"n2"|"n1"}
Estimate the lowest level where a learner would typically know this word.
If proper noun / unknown, prefer n1 if advanced news, else n2 if intermediate, n3 if common.
No markdown, no explanation.`,
		lemma, reading,
	)
}

// Classify implements Classifier.
func (h *HeadlessClassifier) Classify(ctx context.Context, lemma, reading string) (Level, error) {
	if h == nil {
		return "", fmt.Errorf("headless classifier: nil")
	}
	lemma = strings.TrimSpace(lemma)
	reading = strings.TrimSpace(reading)
	if lemma == "" {
		return "", fmt.Errorf("headless classifier: empty lemma")
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	timeout := h.cfg.Timeout
	if timeout <= 0 {
		timeout = 12 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	prompt := ClassifyPrompt(lemma, reading)
	args := h.BuildArgs(prompt)

	var out []byte
	var err error
	if h.cfg.Runner != nil {
		out, err = h.cfg.Runner(cctx, h.cfg.Bin, args...)
	} else {
		cmd := exec.CommandContext(cctx, h.cfg.Bin, args...)
		var buf bytes.Buffer
		cmd.Stdout = &buf
		cmd.Stderr = &buf
		err = cmd.Run()
		out = buf.Bytes()
	}
	if err != nil {
		return "", fmt.Errorf("grok headless: %w (out=%s)", err, truncate(string(out), 200))
	}
	return ParseClassifyOutput(string(out))
}

// ParseClassifyOutput extracts level from headless stdout (JSON object or nested result text).
func ParseClassifyOutput(raw string) (Level, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty classifier output")
	}

	// Direct {"level":"n1"}
	var direct struct {
		Level string `json:"level"`
	}
	if err := json.Unmarshal([]byte(raw), &direct); err == nil {
		if lv, ok := ParseLevel(direct.Level); ok {
			return lv, nil
		}
	}

	// Headless --output-format json may wrap the agent result.
	var wrap map[string]any
	if err := json.Unmarshal([]byte(raw), &wrap); err == nil {
		for _, key := range []string{"result", "text", "message", "content", "output"} {
			if v, ok := wrap[key]; ok {
				if s, ok := v.(string); ok {
					if lv, err := ParseClassifyOutput(s); err == nil {
						return lv, nil
					}
					// Try to find JSON object inside prose.
					if lv, err := parseLevelFromProse(s); err == nil {
						return lv, nil
					}
				}
			}
		}
	}

	if lv, err := parseLevelFromProse(raw); err == nil {
		return lv, nil
	}
	return "", fmt.Errorf("cannot parse level from: %s", truncate(raw, 200))
}

func parseLevelFromProse(s string) (Level, error) {
	// Find first {"level":"..."} substring.
	start := strings.Index(s, `{"level"`)
	if start < 0 {
		start = strings.Index(s, `{"level":`)
	}
	if start < 0 {
		// bare n1..n5 token
		for _, cand := range []string{"n1", "n2", "n3", "n4", "n5"} {
			if strings.Contains(strings.ToLower(s), `"`+cand+`"`) || strings.Contains(strings.ToLower(s), ":"+cand) {
				if lv, ok := ParseLevel(cand); ok {
					return lv, nil
				}
			}
		}
		return "", fmt.Errorf("no level in prose")
	}
	end := strings.Index(s[start:], "}")
	if end < 0 {
		return "", fmt.Errorf("unterminated json")
	}
	chunk := s[start : start+end+1]
	var direct struct {
		Level string `json:"level"`
	}
	if err := json.Unmarshal([]byte(chunk), &direct); err != nil {
		return "", err
	}
	lv, ok := ParseLevel(direct.Level)
	if !ok {
		return "", fmt.Errorf("bad level %q", direct.Level)
	}
	return lv, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// LookPath reports whether bin is on PATH.
func LookPath(bin string) bool {
	if bin == "" {
		bin = "grok"
	}
	_, err := exec.LookPath(bin)
	return err == nil
}
