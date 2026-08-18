// Package agentoutcome implements the isolated smevals fake-executor adapter.
package agentoutcome

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/stacklok/matlatl/eval/internal/evalfs"
	"github.com/stacklok/matlatl/eval/internal/harness"
	"github.com/stacklok/matlatl/eval/internal/manifest"
)

const (
	// OpenCodeVersion is the pinned OpenCode protocol version used by the adapter.
	OpenCodeVersion = "1.18.18"
	// SentinelModel prevents accidental use without an explicit model override.
	SentinelModel  = "REQUIRES_EXPLICIT_MODEL_OVERRIDE"
	maxOutputBytes = 1 << 20
	maxEvents      = 256
	// PilotPointer is the fixed treatment notice appended to all-arm prompts.
	PilotPointer = "Use the generated llms.txt and trails.json files and the configured matlatl MCP tools when useful."
)

var errEventBudget = errors.New("event budget exhausted")

// Environment contains the validated inputs for an isolated agent run.
type Environment struct {
	Model, Prompt, Task, RunDir, EvalRoot, Arm                  string
	Runtime, Image, ScheduledRunID, AttemptID, RetryParent      string
	FakeMode, HostGoldSentinel, HostTempSentinel, NetworkCanary string
	Timeout                                                     time.Duration
}

// Preparation records the treatment prepared for an agent run.
type Preparation struct {
	SchemaVersion   int      `json:"schemaVersion"`
	Task            string   `json:"task"`
	Arm             string   `json:"arm"`
	Model           string   `json:"model"`
	OpenCodeVersion string   `json:"openCodeVersion"`
	Artifacts       []string `json:"artifacts"`
	MCP             bool     `json:"mcp"`
	PointerPresent  bool     `json:"pointerPresent"`
	PointerSHA256   string   `json:"pointerSHA256"`
	Notice          string   `json:"notice,omitempty"`
}

// Outcome records the terminal status and answer of an agent run.
type Outcome struct {
	SchemaVersion int             `json:"schemaVersion"`
	Status        manifest.Status `json:"status"`
	Answer        string          `json:"answer"`
}

// Metrics records bounded usage reported by an agent run.
type Metrics struct {
	SchemaVersion    int     `json:"schemaVersion"`
	Events           int     `json:"events"`
	ToolCalls        int     `json:"toolCalls"`
	InputTokens      int64   `json:"inputTokens"`
	OutputTokens     int64   `json:"outputTokens"`
	CacheReadTokens  int64   `json:"cacheReadTokens"`
	CacheWriteTokens int64   `json:"cacheWriteTokens"`
	Cost             float64 `json:"cost"`
}

// Failure records a harness failure that prevented a gradeable outcome.
type Failure struct {
	SchemaVersion int             `json:"schemaVersion"`
	Status        manifest.Status `json:"status"`
	Message       string          `json:"message"`
}

// ReadEnvironment reads and validates the adapter environment.
func ReadEnvironment() (Environment, error) {
	timeout := 30 * time.Second
	if value := os.Getenv("MATLATL_AGENT_TIMEOUT_MS"); value != "" {
		ms, err := strconv.Atoi(value)
		if err != nil || ms < 1 || ms > 300000 {
			return Environment{}, errors.New("MATLATL_AGENT_TIMEOUT_MS must be between 1 and 300000")
		}
		timeout = time.Duration(ms) * time.Millisecond
	}
	env := Environment{Model: os.Getenv("SMEVALS_MODEL"), Prompt: os.Getenv("SMEVALS_PROMPT"), Task: os.Getenv("SMEVALS_TASK"), RunDir: os.Getenv("SMEVALS_RUN_DIR"), EvalRoot: os.Getenv("MATLATL_EVAL_ROOT"), Arm: os.Getenv("MATLATL_AGENT_ARM"), Runtime: os.Getenv("MATLATL_OCI_RUNTIME"), Image: os.Getenv("MATLATL_OCI_IMAGE"), ScheduledRunID: os.Getenv("MATLATL_SCHEDULED_RUN_ID"), AttemptID: os.Getenv("MATLATL_ATTEMPT_ID"), RetryParent: os.Getenv("MATLATL_RETRY_PARENT"), FakeMode: os.Getenv("MATLATL_FAKE_MODE"), Timeout: timeout}
	missing := []string{}
	for _, item := range []struct{ name, value string }{{"SMEVALS_MODEL", env.Model}, {"SMEVALS_PROMPT", env.Prompt}, {"SMEVALS_TASK", env.Task}, {"SMEVALS_RUN_DIR", env.RunDir}, {"MATLATL_EVAL_ROOT", env.EvalRoot}, {"MATLATL_AGENT_ARM", env.Arm}, {"MATLATL_OCI_IMAGE", env.Image}, {"MATLATL_SCHEDULED_RUN_ID", env.ScheduledRunID}, {"MATLATL_ATTEMPT_ID", env.AttemptID}} {
		if item.value == "" {
			missing = append(missing, item.name)
		}
	}
	if len(missing) > 0 {
		return Environment{}, fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}
	if !validRunID(env.ScheduledRunID) || !validRunID(env.AttemptID) || (env.RetryParent != "" && !validRunID(env.RetryParent)) {
		return Environment{}, errors.New("run identity variables contain invalid characters or length")
	}
	if len(env.Prompt) > manifest.MaxPayloadBytes {
		return Environment{}, fmt.Errorf("SMEVALS_PROMPT exceeds %d-byte limit", manifest.MaxPayloadBytes)
	}
	return env, nil
}

// Run executes an isolated agent run using the selected OCI runtime.
func Run(ctx context.Context, env Environment) (Outcome, Metrics, bool, error) {
	oci, err := SelectRuntime(env.Runtime)
	if err != nil {
		root, rootErr := evalfs.Root(env.RunDir)
		if rootErr != nil {
			return Outcome{}, Metrics{}, false, errors.Join(err, rootErr)
		}
		return fail(root, manifest.StatusEnvironmentFailure, err)
	}
	return isolatedRun(ctx, env, oci)
}

// RunWithRuntime executes an isolated agent run through the supplied runtime seam.
func RunWithRuntime(ctx context.Context, env Environment, oci OCIRuntime) (Outcome, Metrics, bool, error) {
	return isolatedRun(ctx, env, oci)
}

func parseEvents(raw []byte) (string, Metrics, error) {
	metrics := Metrics{SchemaVersion: 1}
	answer := ""
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 4096), manifest.MaxPayloadBytes)
	for scanner.Scan() {
		if metrics.Events == maxEvents {
			return "", metrics, errEventBudget
		}
		var event map[string]any
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return "", metrics, err
		}
		metrics.Events++
		typ, _ := event["type"].(string)
		part, _ := event["part"].(map[string]any)
		if typ == "text" {
			if text, ok := part["text"].(string); ok && strings.TrimSpace(text) != "" {
				answer = text
			}
		}
		if typ == "tool_use" {
			metrics.ToolCalls++
		}
		if typ == "step_finish" {
			addUsage(&metrics, part)
		}
	}
	if scanner.Err() != nil {
		return "", metrics, errEventBudget
	}
	if metrics.Events == 0 || strings.TrimSpace(answer) == "" {
		return "", metrics, errors.New("no final text event")
	}
	return strings.TrimSpace(answer), metrics, nil
}
func addUsage(metrics *Metrics, part map[string]any) {
	if value, ok := number(part["cost"]); ok {
		metrics.Cost += value
	}
	tokens, _ := part["tokens"].(map[string]any)
	metrics.InputTokens += integer(tokens["input"])
	metrics.OutputTokens += integer(tokens["output"])
	cache, _ := tokens["cache"].(map[string]any)
	metrics.CacheReadTokens += integer(cache["read"])
	metrics.CacheWriteTokens += integer(cache["write"])
}
func number(value any) (float64, bool) { v, ok := value.(float64); return v, ok }
func integer(value any) int64          { v, _ := number(value); return int64(v) }

func recordAgent(root string, _ []byte, outcome Outcome, metrics Metrics) (Outcome, Metrics, bool, error) {
	if metrics.SchemaVersion == 0 {
		metrics.SchemaVersion = 1
	}
	if err := writeJSON(root, "outcome.json", outcome); err != nil {
		_, _, _, failErr := fail(root, manifest.StatusEvaluatorFailure, fmt.Errorf("persist outcome: %w", err))
		return Outcome{}, Metrics{}, false, failErr
	}
	if err := writeJSON(root, "metrics.json", metrics); err != nil {
		_, _, _, failErr := fail(root, manifest.StatusEvaluatorFailure, fmt.Errorf("persist metrics: %w", err))
		return Outcome{}, Metrics{}, false, failErr
	}
	return outcome, metrics, true, nil
}
func fail(root string, status manifest.Status, cause error) (Outcome, Metrics, bool, error) {
	if writeErr := writeFailure(root, status, cause); writeErr != nil {
		return Outcome{}, Metrics{}, false, errors.Join(cause, writeErr)
	}
	return Outcome{}, Metrics{}, false, cause
}

// RecordFailure persists a bounded failure record beneath runDir.
func RecordFailure(runDir string, status manifest.Status, cause error) error {
	root, err := evalfs.Root(runDir)
	if err != nil {
		return err
	}
	return writeFailure(root, status, cause)
}
func writeFailure(root string, status manifest.Status, cause error) error {
	return writeJSON(root, "failure.json", Failure{SchemaVersion: 1, Status: status, Message: boundedMessage(cause.Error())})
}
func boundedMessage(message string) string {
	message = strings.ReplaceAll(message, "\n", " ")
	if len(message) > 4096 {
		return message[:4096]
	}
	return message
}
func writeJSON(root, name string, value any) error {
	content, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return evalfs.WriteExclusive(root, name, append(content, '\n'))
}
func loadTask(evalRoot, name string) (*manifest.Task, error) {
	root, err := evalfs.Root(evalRoot)
	if err != nil {
		return nil, err
	}
	if name != "canonical-navigation" {
		return nil, fmt.Errorf("unknown task %q", name)
	}
	content, err := evalfs.Read(root, "tasks/canonical-navigation/v1/task.json")
	if err != nil {
		return nil, err
	}
	return manifest.DecodeTask(bytes.NewReader(content))
}

func decodeOutcome(content []byte) (*Outcome, error) {
	if len(content) > manifest.MaxPayloadBytes {
		return nil, fmt.Errorf("outcome.json exceeds %d-byte limit", manifest.MaxPayloadBytes)
	}
	var outcome Outcome
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&outcome); err != nil {
		return nil, fmt.Errorf("decode outcome.json: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("decode outcome.json: trailing JSON value")
		}
		return nil, fmt.Errorf("decode outcome.json trailing data: %w", err)
	}
	if outcome.SchemaVersion != 1 {
		return nil, fmt.Errorf("outcome.json schemaVersion %d, require 1", outcome.SchemaVersion)
	}
	if !validGradeableOutcomeStatus(outcome.Status) {
		return nil, fmt.Errorf("outcome.json has invalid status %q", outcome.Status)
	}
	if len(outcome.Answer) > manifest.MaxPayloadBytes {
		return nil, errors.New("outcome answer exceeds byte limit")
	}
	if outcome.Status == manifest.StatusCompleted && strings.TrimSpace(outcome.Answer) == "" {
		return nil, errors.New("completed outcome requires a non-empty answer")
	}
	if outcome.Status != manifest.StatusCompleted && outcome.Answer != "" {
		return nil, fmt.Errorf("%s outcome must have an empty answer", outcome.Status)
	}
	return &outcome, nil
}

func validGradeableOutcomeStatus(status manifest.Status) bool {
	switch status {
	case manifest.StatusCompleted, manifest.StatusAgentTimeout, manifest.StatusBudgetExhausted,
		manifest.StatusAgentProtocolFailure, manifest.StatusEnvironmentFailure, manifest.StatusMCPFailure,
		manifest.StatusProviderFailure, manifest.StatusEvaluatorFailure:
		return true
	default:
		return false
	}
}

// Check scores a persisted run against its authoritative task oracle.
func Check(runDir, evalRoot, taskName string) (map[string]any, bool, error) {
	task, err := loadTask(evalRoot, taskName)
	if err != nil {
		return nil, false, err
	}
	runRoot, err := evalfs.Root(runDir)
	if err != nil {
		return nil, false, err
	}
	content, err := evalfs.Read(runRoot, "outcome.json")
	if err != nil {
		return nil, false, err
	}
	outcome, err := decodeOutcome(content)
	if err != nil {
		return nil, false, err
	}
	score, err := (harness.ExactPathScorer{GoldRoot: filepath.Join(evalRoot, "gold")}).Score(task, outcome.Status, outcome.Answer)
	if err != nil {
		return nil, false, err
	}
	passed := score == 1
	result := map[string]any{"score": float64(0), "metrics": map[string]any{"exact_path": false}, "tags": []string{"wrong_answer"}}
	if passed {
		result = map[string]any{"score": float64(1), "metrics": map[string]any{"exact_path": true}, "tags": []string{}}
	} else if outcome.Status != manifest.StatusCompleted {
		result["tags"] = []string{string(outcome.Status)}
	}
	return result, passed, nil
}

type limitedBuffer struct {
	mu       sync.Mutex
	buffer   bytes.Buffer
	limit    int
	exceeded bool
	overflow func()
}

func (w *limitedBuffer) CopyBytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.buffer.Bytes()...)
}
func (w *limitedBuffer) Len() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buffer.Len()
}
func (w *limitedBuffer) Exceeded() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.exceeded
}
func (w *limitedBuffer) Write(p []byte) (int, error) {
	w.mu.Lock()
	remaining := w.limit - w.buffer.Len()
	switch {
	case remaining <= 0:
		w.exceeded = true
	case len(p) > remaining:
		_, _ = w.buffer.Write(p[:remaining])
		w.exceeded = true
	default:
		_, _ = w.buffer.Write(p)
	}
	exceeded, overflow := w.exceeded, w.overflow
	w.mu.Unlock()
	if exceeded && overflow != nil {
		overflow()
	}
	return len(p), nil
}

var _ io.Writer = (*limitedBuffer)(nil)
