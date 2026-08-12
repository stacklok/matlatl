package manifest

import (
	"bytes"
	"strings"
	"testing"
)

func validTask() *Task {
	return &Task{
		SchemaVersion: SchemaVersion, ID: "task", Version: "v1", Kind: "navigation",
		Instruction: "find [[marker]]", CorpusRef: "corpus/v1", GoldRef: "task/v1",
		AnswerFormat: "path",
	}
}

func resultPair(t *testing.T, run string, attempt int, status Status) (*Result, *Trajectory) {
	t.Helper()
	taskHash, err := TaskHash(validTask())
	if err != nil {
		t.Fatal(err)
	}
	id := AttemptID(run, taskHash, "baseline", "mock", attempt)
	result := &Result{
		SchemaVersion: SchemaVersion, RunID: run, AttemptID: id, TaskID: "task",
		TaskHash: taskHash, Arm: "baseline", AgentID: "mock", Attempt: attempt,
		Status: status, Score: -1,
	}
	if status == StatusCompleted {
		result.Answer, result.Score = "doc.md", 1
	}
	trajectory := &Trajectory{
		SchemaVersion: SchemaVersion, RunID: run, AttemptID: id, TaskHash: taskHash,
		Arm: "baseline", AgentID: "mock", Events: []Event{{Sequence: 1, Kind: "answer", Payload: result.Answer}},
	}
	if _, err := SealResult(result); err != nil {
		t.Fatal(err)
	}
	if _, err := SealTrajectory(trajectory); err != nil {
		t.Fatal(err)
	}
	return result, trajectory
}

func reseal(t *testing.T, results []*Result, trajectories []*Trajectory) {
	t.Helper()
	for _, result := range results {
		if _, err := SealResult(result); err != nil {
			t.Fatal(err)
		}
	}
	for _, trajectory := range trajectories {
		if _, err := SealTrajectory(trajectory); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStrictDecodingAndHash(t *testing.T) {
	canonical, err := MarshalTask(validTask())
	if err != nil {
		t.Fatal(err)
	}
	cases := [][]byte{
		[]byte("{"),
		append(append([]byte{}, canonical...), []byte("{}")...),
		[]byte(`{"schemaVersion":1,"id":"x","version":"v1","kind":"navigation","instruction":"find [[x]]","corpusRef":"c","goldRef":"g","answerFormat":"path","unknown":true}`),
		bytes.Replace(canonical, []byte(`"schemaVersion": 1`), []byte(`"schemaVersion": 2`), 1),
	}
	for i, input := range cases {
		if _, err := DecodeTask(bytes.NewReader(input)); err == nil {
			t.Errorf("case %d decoded", i)
		}
	}
	result, _ := resultPair(t, "run", 1, StatusCompleted)
	result.Hash = strings.Repeat("0", 64)
	encoded, err := encode(result)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeResult(bytes.NewReader(encoded)); err == nil {
		t.Fatal("bad result hash decoded")
	}
}

func TestCanonicalTaskEncoding(t *testing.T) {
	task := validTask()
	first, err := MarshalTask(task)
	if err != nil {
		t.Fatal(err)
	}
	second, err := MarshalTask(task)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("canonical bytes differ:\n%s\n%s", first, second)
	}
	firstHash, _ := TaskHash(task)
	secondHash, _ := TaskHash(task)
	if firstHash != secondHash {
		t.Fatal("canonical hashes differ")
	}
}

func TestTaskRejectsMaliciousCorpusPath(t *testing.T) {
	for _, ref := range []string{"../gold/task", "/gold/task", "gold/task", "corpus/../../gold"} {
		task := validTask()
		task.CorpusRef = ref
		if err := ValidateTask(task); err == nil {
			t.Errorf("accepted corpusRef %q", ref)
		}
	}
}

func TestTrajectoryEventsMustBeContiguous(t *testing.T) {
	_, trajectory := resultPair(t, "run", 1, StatusCompleted)
	trajectory.Events[0].Sequence = 2
	_, _ = SealTrajectory(trajectory)
	if err := ValidateTrajectory(trajectory); err == nil {
		t.Fatal("accepted non-contiguous events")
	}
}

func TestRecordRelationships(t *testing.T) {
	base, baseTrajectory := resultPair(t, "run", 1, StatusEnvironmentFailure)
	retry, retryTrajectory := resultPair(t, "run", 2, StatusCompleted)
	retry.RetryParent = base.AttemptID
	reseal(t, []*Result{retry}, nil)
	if err := ValidateRecords([]*Result{base, retry}, []*Trajectory{baseTrajectory, retryTrajectory}); err != nil {
		t.Fatalf("valid retry: %v", err)
	}

	tests := map[string]func([]*Result, []*Trajectory){
		"duplicate":      func(results []*Result, _ []*Trajectory) { results[1] = results[0] },
		"missing parent": func(results []*Result, _ []*Trajectory) { results[1].RetryParent = "missing" },
		"self parent":    func(results []*Result, _ []*Trajectory) { results[1].RetryParent = results[1].AttemptID },
		"cross run": func(results []*Result, trajectories []*Trajectory) {
			results[1].RunID = "other"
			trajectories[1].RunID = "other"
		},
		"forward parent": func(results []*Result, _ []*Trajectory) {
			results[0].RetryParent = results[1].AttemptID
			results[1].RetryParent = ""
		},
		"non-retryable": func(results []*Result, _ []*Trajectory) { results[0].Status, results[0].Score = StatusCompleted, 1 },
		"mismatch":      func(_ []*Result, trajectories []*Trajectory) { trajectories[1].Arm = "other" },
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			r1, tr1 := resultPair(t, "run", 1, StatusEnvironmentFailure)
			r2, tr2 := resultPair(t, "run", 2, StatusCompleted)
			r2.RetryParent = r1.AttemptID
			results, trajectories := []*Result{r1, r2}, []*Trajectory{tr1, tr2}
			mutate(results, trajectories)
			reseal(t, results, trajectories)
			if err := ValidateRecords(results, trajectories); err == nil {
				t.Fatal("invalid records accepted")
			}
		})
	}
}

func TestInfraExhaustedAndOrphanTrajectory(t *testing.T) {
	result, trajectory := resultPair(t, "run", 1, StatusInfraExhausted)
	if err := ValidateRecords([]*Result{result}, []*Trajectory{trajectory}); err != nil {
		t.Fatalf("infra-exhausted terminal result: %v", err)
	}
	if err := ValidateRecords(nil, []*Trajectory{trajectory}); err == nil || !strings.Contains(err.Error(), "without matching result") {
		t.Fatalf("orphan trajectory error = %v", err)
	}
}

func TestCyclicRetryRelationship(t *testing.T) {
	r1, tr1 := resultPair(t, "run", 1, StatusEnvironmentFailure)
	r2, tr2 := resultPair(t, "run", 2, StatusEnvironmentFailure)
	r1.RetryParent = r2.AttemptID
	r2.RetryParent = r1.AttemptID
	reseal(t, []*Result{r1, r2}, []*Trajectory{tr1, tr2})
	if err := ValidateRecords([]*Result{r1, r2}, []*Trajectory{tr1, tr2}); err == nil || !strings.Contains(err.Error(), "cyclic") {
		t.Fatalf("cycle error = %v", err)
	}
}
