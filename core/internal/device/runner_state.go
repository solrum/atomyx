package device

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// RunnerStateSuffix is appended to a device's lease key to name the file
// that records the iOS runner launched for that device.
const RunnerStateSuffix = ".ios-runner.json"

// runnerStateFileMode keeps the record private to the current user.
const runnerStateFileMode = 0o600

// RunnerState is the record of one launched iOS runner, written when the
// runner process starts and removed when the runner is stopped. A file
// left behind therefore means the process that launched the runner ended
// without stopping it.
//
// Pid and Pgid identify the runner's process group. The runner always
// leads its own process group, so the two are equal. ProcessStartTime
// and HostBootTime let a later reader confirm a live pid is still the
// process recorded here and not an unrelated one that reused the pid.
type RunnerState struct {
	RunnerID         string    `json:"runnerId"`
	HostPort         int       `json:"hostPort"`
	Pid              int       `json:"pid"`
	Pgid             int       `json:"pgid"`
	ProcessStartTime time.Time `json:"processStartTime"`
	HostBootTime     time.Time `json:"hostBootTime"`
	StartedAt        time.Time `json:"startedAt"`
}

// RunnerStatePath returns the path of the runner record for key inside
// dir.
func RunnerStatePath(dir, key string) string {
	return filepath.Join(dir, key+RunnerStateSuffix)
}

// WriteRunnerState writes state to path atomically, so a concurrent
// reader sees either the previous record or the complete new one.
func WriteRunnerState(path string, state RunnerState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("encode the runner state: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create the runner state file: %w", err)
	}
	tmpPath := tmp.Name()
	if err := os.Chmod(tmpPath, runnerStateFileMode); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("restrict the runner state file: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write the runner state file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write the runner state file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("publish the runner state file: %w", err)
	}
	return nil
}

// ReadRunnerState reads the record at path. A missing file reports
// os.ErrNotExist through errors.Is.
func ReadRunnerState(path string) (RunnerState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return RunnerState{}, err
	}
	var state RunnerState
	if err := json.Unmarshal(data, &state); err != nil {
		return RunnerState{}, fmt.Errorf("decode the runner state file %s: %w", path, err)
	}
	return state, nil
}

// removeRunnerStateOf deletes the record at path when it still names
// runnerID, so stopping one runner never removes the record of a runner
// launched after it.
func removeRunnerStateOf(path, runnerID string) {
	state, err := ReadRunnerState(path)
	if err != nil || state.RunnerID != runnerID {
		return
	}
	_ = os.Remove(path)
}
