// Package manifest defines and validates the versioned evaluation records.
package manifest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"
)

const (
	// SchemaVersion is the current task, result, and trajectory schema version.
	SchemaVersion = 1
	// MaxStringBytes bounds identifiers, paths, and other short manifest strings.
	MaxStringBytes = 16 << 10
	// MaxPayloadBytes bounds instructions, answers, and individual event payloads.
	MaxPayloadBytes = 64 << 10
	// MaxEventsPerTrajectory bounds one decoded trajectory.
	MaxEventsPerTrajectory = 256
	// MaxRecordCollectionBytes bounds all result and trajectory JSON loaded at once.
	MaxRecordCollectionBytes = 2 << 20
	// MaxRecordCollectionEvents bounds events across all loaded trajectories.
	MaxRecordCollectionEvents = 4096
)

// Task is an immutable task specification.
type Task struct {
	SchemaVersion int    `json:"schemaVersion"`
	ID            string `json:"id"`
	Version       string `json:"version"`
	Kind          string `json:"kind"`
	Instruction   string `json:"instruction"`
	CorpusRef     string `json:"corpusRef"`
	GoldRef       string `json:"goldRef"`
	AnswerFormat  string `json:"answerFormat"`
}

// Status is an attempt's terminal classification.
type Status string

const (
	// StatusCompleted means the agent produced a scoreable answer.
	StatusCompleted Status = "completed"
	// StatusAgentTimeout means the agent exceeded its time limit.
	StatusAgentTimeout Status = "agent-timeout"
	// StatusBudgetExhausted means the agent exhausted a frozen budget.
	StatusBudgetExhausted Status = "budget-exhausted"
	// StatusAgentProtocolFailure means agent output violated the protocol.
	StatusAgentProtocolFailure Status = "agent-protocol-failure"
	// StatusEnvironmentFailure means the prepared environment failed.
	StatusEnvironmentFailure Status = "environment-failure"
	// StatusMCPFailure means the configured MCP service failed.
	StatusMCPFailure Status = "mcp-failure"
	// StatusProviderFailure means the model provider failed.
	StatusProviderFailure Status = "provider-failure"
	// StatusEvaluatorFailure means the harness or scorer failed.
	StatusEvaluatorFailure Status = "evaluator-failure"
	// StatusInvalidTask means the task or its private evaluation data is invalid.
	StatusInvalidTask Status = "invalid-task"
	// StatusInfraExhausted means bounded retryable failures exhausted all attempts.
	StatusInfraExhausted Status = "infra-exhausted"
)

var allStatuses = []Status{
	StatusCompleted, StatusAgentTimeout, StatusBudgetExhausted,
	StatusAgentProtocolFailure, StatusEnvironmentFailure, StatusMCPFailure,
	StatusProviderFailure, StatusEvaluatorFailure, StatusInvalidTask, StatusInfraExhausted,
}

var retryableStatuses = []Status{
	StatusEnvironmentFailure, StatusMCPFailure, StatusProviderFailure, StatusEvaluatorFailure,
}

// IsAgentOutcomeStatus reports whether an agent may report status directly.
// Infrastructure, provider, evaluator, invalid-task, and exhausted statuses are
// assigned by the surrounding harness, never by an agent.
func IsAgentOutcomeStatus(status Status) bool {
	switch status {
	case StatusCompleted, StatusAgentTimeout, StatusBudgetExhausted, StatusAgentProtocolFailure:
		return true
	default:
		return false
	}
}

// Event is one ordered trajectory event.
type Event struct {
	Sequence int    `json:"sequence"`
	Kind     string `json:"kind"`
	Payload  string `json:"payload"`
}

// Trajectory is an immutable attempt event log.
type Trajectory struct {
	SchemaVersion int     `json:"schemaVersion"`
	RunID         string  `json:"runId"`
	AttemptID     string  `json:"attemptId"`
	TaskHash      string  `json:"taskHash"`
	Arm           string  `json:"arm"`
	AgentID       string  `json:"agentId"`
	Events        []Event `json:"events"`
	Hash          string  `json:"hash"`
}

// Result is an immutable attempt outcome.
type Result struct {
	SchemaVersion int    `json:"schemaVersion"`
	RunID         string `json:"runId"`
	AttemptID     string `json:"attemptId"`
	TaskID        string `json:"taskId"`
	TaskHash      string `json:"taskHash"`
	Arm           string `json:"arm"`
	AgentID       string `json:"agentId"`
	Attempt       int    `json:"attempt"`
	Status        Status `json:"status"`
	Answer        string `json:"answer"`
	Score         int    `json:"score"`
	RetryParent   string `json:"retryParent"`
	Hash          string `json:"hash"`
}

// AttemptID deterministically identifies one scheduled attempt.
func AttemptID(runID, taskHash, arm, agentID string, attempt int) string {
	return digest([]byte(fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%d", runID, taskHash, arm, agentID, attempt)))
}

// TaskHash hashes the canonical task representation.
func TaskHash(task *Task) (string, error) {
	b, err := MarshalTask(task)
	if err != nil {
		return "", err
	}
	return digest(b), nil
}

// MarshalTask returns deterministic JSON.
func MarshalTask(task *Task) ([]byte, error) {
	if err := ValidateTask(task); err != nil {
		return nil, err
	}
	return encode(task)
}

// SealTrajectory sets and returns the canonical content hash.
func SealTrajectory(trajectory *Trajectory) (string, error) {
	if trajectory == nil {
		return "", errors.New("manifest: nil trajectory")
	}
	trajectory.Hash = ""
	b, err := encode(trajectory)
	if err != nil {
		return "", err
	}
	trajectory.Hash = digest(b)
	return trajectory.Hash, nil
}

// SealResult sets and returns the canonical content hash.
func SealResult(result *Result) (string, error) {
	if result == nil {
		return "", errors.New("manifest: nil result")
	}
	result.Hash = ""
	b, err := encode(result)
	if err != nil {
		return "", err
	}
	result.Hash = digest(b)
	return result.Hash, nil
}

// MarshalTrajectory returns canonical, validated JSON.
func MarshalTrajectory(trajectory *Trajectory) ([]byte, error) {
	if err := ValidateTrajectory(trajectory); err != nil {
		return nil, err
	}
	return encode(trajectory)
}

// MarshalResult returns canonical, validated JSON.
func MarshalResult(result *Result) ([]byte, error) {
	if err := ValidateResult(result); err != nil {
		return nil, err
	}
	return encode(result)
}

// DecodeTask strictly decodes one task JSON value.
func DecodeTask(r io.Reader) (*Task, error) {
	var task Task
	if err := decode(r, &task); err != nil {
		return nil, fmt.Errorf("manifest: decode task: %w", err)
	}
	if err := ValidateTask(&task); err != nil {
		return nil, err
	}
	return &task, nil
}

// DecodeTrajectory strictly decodes and validates one trajectory.
func DecodeTrajectory(r io.Reader) (*Trajectory, error) {
	var trajectory Trajectory
	if err := decode(r, &trajectory); err != nil {
		return nil, fmt.Errorf("manifest: decode trajectory: %w", err)
	}
	if err := ValidateTrajectory(&trajectory); err != nil {
		return nil, err
	}
	return &trajectory, nil
}

// DecodeResult strictly decodes and validates one result.
func DecodeResult(r io.Reader) (*Result, error) {
	var result Result
	if err := decode(r, &result); err != nil {
		return nil, fmt.Errorf("manifest: decode result: %w", err)
	}
	if err := ValidateResult(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

// ValidateTask enforces the v1 task semantics and private-data boundary.
func ValidateTask(task *Task) error {
	if task == nil {
		return errors.New("manifest: nil task")
	}
	if task.SchemaVersion != SchemaVersion {
		return fmt.Errorf("manifest: unsupported task schemaVersion %d", task.SchemaVersion)
	}
	if task.ID == "" || task.Version == "" || task.Kind != "navigation" || task.Instruction == "" {
		return errors.New("manifest: task identity, navigation kind, and instruction are required")
	}
	if !shortStrings(task.ID, task.Version, task.Kind, task.CorpusRef, task.GoldRef, task.AnswerFormat) || len(task.Instruction) > MaxPayloadBytes {
		return errors.New("manifest: task string exceeds v1 size limit")
	}
	if task.AnswerFormat != "path" {
		return fmt.Errorf("manifest: unsupported answer format %q", task.AnswerFormat)
	}
	if !safeRef(task.CorpusRef) || !safeRef(task.GoldRef) {
		return errors.New("manifest: corpusRef and goldRef must be safe relative paths")
	}
	if task.CorpusRef == task.GoldRef || strings.Contains(strings.ToLower(task.CorpusRef), "gold") {
		return errors.New("manifest: corpusRef must not expose private gold")
	}
	return nil
}

// ValidateTrajectory validates event continuity and the canonical hash.
func ValidateTrajectory(trajectory *Trajectory) error {
	if trajectory == nil || trajectory.SchemaVersion != SchemaVersion {
		return errors.New("manifest: unsupported or nil trajectory")
	}
	if trajectory.RunID == "" || trajectory.AttemptID == "" || trajectory.TaskHash == "" || trajectory.Arm == "" || trajectory.AgentID == "" {
		return errors.New("manifest: incomplete trajectory identity")
	}
	if !shortStrings(trajectory.RunID, trajectory.AttemptID, trajectory.TaskHash, trajectory.Arm, trajectory.AgentID, trajectory.Hash) {
		return errors.New("manifest: trajectory string exceeds v1 size limit")
	}
	if len(trajectory.Events) > MaxEventsPerTrajectory {
		return fmt.Errorf("manifest: trajectory exceeds %d events", MaxEventsPerTrajectory)
	}
	for i, event := range trajectory.Events {
		if event.Sequence != i+1 || event.Kind == "" {
			return fmt.Errorf("manifest: non-contiguous or invalid event at index %d", i)
		}
		if !shortStrings(event.Kind) || len(event.Payload) > MaxPayloadBytes {
			return fmt.Errorf("manifest: event %d string exceeds v1 size limit", i+1)
		}
	}
	return checkHash(trajectory.Hash, func() ([]byte, error) {
		copy := *trajectory
		copy.Hash = ""
		return encode(copy)
	})
}

// ValidateResult validates status/score semantics and the canonical hash.
func ValidateResult(result *Result) error {
	if result == nil || result.SchemaVersion != SchemaVersion {
		return errors.New("manifest: unsupported or nil result")
	}
	if result.RunID == "" || result.AttemptID == "" || result.TaskID == "" || result.TaskHash == "" || result.Arm == "" || result.AgentID == "" || result.Attempt < 1 {
		return errors.New("manifest: incomplete result identity")
	}
	if !shortStrings(result.RunID, result.AttemptID, result.TaskID, result.TaskHash, result.Arm, result.AgentID, result.RetryParent, result.Hash) || len(result.Answer) > MaxPayloadBytes {
		return errors.New("manifest: result string exceeds v1 size limit")
	}
	if !slices.Contains(allStatuses, result.Status) {
		return fmt.Errorf("manifest: unknown status %q", result.Status)
	}
	if result.Status == StatusCompleted {
		if result.Score != 0 && result.Score != 1 {
			return errors.New("manifest: completed score must be 0 or 1")
		}
	} else if result.Score != -1 || result.Answer != "" {
		return errors.New("manifest: incomplete attempt must be unscored with no answer")
	}
	return checkHash(result.Hash, func() ([]byte, error) {
		copy := *result
		copy.Hash = ""
		return encode(copy)
	})
}

// ValidateRecords checks duplicate attempts, result/trajectory agreement, and
// the v1 retry-parent rules.
func ValidateRecords(results []*Result, trajectories []*Trajectory) error {
	trajectoryByID := make(map[string]*Trajectory, len(trajectories))
	for _, trajectory := range trajectories {
		if err := ValidateTrajectory(trajectory); err != nil {
			return err
		}
		if _, exists := trajectoryByID[trajectory.AttemptID]; exists {
			return fmt.Errorf("manifest: duplicate trajectory attempt %q", trajectory.AttemptID)
		}
		trajectoryByID[trajectory.AttemptID] = trajectory
	}
	resultByID := make(map[string]*Result, len(results))
	for _, result := range results {
		if err := ValidateResult(result); err != nil {
			return err
		}
		if _, exists := resultByID[result.AttemptID]; exists {
			return fmt.Errorf("manifest: duplicate result attempt %q", result.AttemptID)
		}
		resultByID[result.AttemptID] = result
		trajectory, exists := trajectoryByID[result.AttemptID]
		if !exists {
			return fmt.Errorf("manifest: missing trajectory for %q", result.AttemptID)
		}
		if trajectory.RunID != result.RunID || trajectory.TaskHash != result.TaskHash || trajectory.Arm != result.Arm || trajectory.AgentID != result.AgentID {
			return fmt.Errorf("manifest: result/trajectory mismatch for %q", result.AttemptID)
		}
	}
	if len(trajectoryByID) != len(results) {
		return errors.New("manifest: trajectory without matching result")
	}
	// Check cycles independently of attempt ordering so corrupt cyclic graphs are
	// classified explicitly rather than relying only on the forward-edge rule.
	for _, start := range results {
		seen := map[string]struct{}{}
		for current := start; current != nil && current.RetryParent != ""; current = resultByID[current.RetryParent] {
			if _, exists := seen[current.AttemptID]; exists {
				return errors.New("manifest: cyclic retry relationship")
			}
			seen[current.AttemptID] = struct{}{}
		}
	}
	for _, result := range results {
		if result.RetryParent == "" {
			continue
		}
		if result.RetryParent == result.AttemptID {
			return errors.New("manifest: self retry parent")
		}
		parent, found := resultByID[result.RetryParent]
		if !found {
			return fmt.Errorf("manifest: missing retry parent %q", result.RetryParent)
		}
		if parent.RunID != result.RunID || parent.TaskHash != result.TaskHash || parent.Arm != result.Arm {
			return errors.New("manifest: cross-run or cross-schedule retry parent")
		}
		if parent.Attempt >= result.Attempt {
			return errors.New("manifest: forward retry parent")
		}
		if !slices.Contains(retryableStatuses, parent.Status) {
			return fmt.Errorf("manifest: retry of non-retryable status %q", parent.Status)
		}
	}
	return nil
}

func shortStrings(values ...string) bool {
	for _, value := range values {
		if len(value) > MaxStringBytes {
			return false
		}
	}
	return true
}

func safeRef(ref string) bool {
	return ref != "" && !strings.HasPrefix(ref, "/") && path.Clean(ref) == ref && ref != "." && ref != ".." && !strings.HasPrefix(ref, "../")
}

func checkHash(stored string, body func() ([]byte, error)) error {
	if stored == "" {
		return errors.New("manifest: missing hash")
	}
	b, err := body()
	if err != nil {
		return err
	}
	if digest(b) != stored {
		return errors.New("manifest: bad hash")
	}
	return nil
}

func decode(r io.Reader, value any) error {
	decoder := json.NewDecoder(r)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON data")
	}
	return nil
}

func encode(value any) ([]byte, error) {
	var b bytes.Buffer
	encoder := json.NewEncoder(&b)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
