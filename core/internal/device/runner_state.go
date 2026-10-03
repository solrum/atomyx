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
// UDID and Kind name the target the runner was launched for, so a
// reader can tell a record that belongs to its device from one that
// does not.
//
// TunnelPid and TunnelStartTime identify the iproxy process that tunnels
// a physical device's runner port, written once the tunnel is up. The
// tunnel leads its own process group. TunnelPid is zero for a simulator
// runner and for a tunnel the launcher reused instead of starting.
//
// OwnerPid and OwnerStartTime identify the process that answers for the
// runner, the one that stops it when it exits. The owner is not the
// runner's parent and not any single session: a session closing leaves
// the runner running, and only the owning process ending, or an owner
// that is no longer alive, makes the runner a candidate for reaping.
type RunnerState struct {
	UDID             string    `json:"udid"`
	Kind             string    `json:"kind"`
	RunnerID         string    `json:"runnerId"`
	HostPort         int       `json:"hostPort"`
	Pid              int       `json:"pid"`
	Pgid             int       `json:"pgid"`
	ProcessStartTime time.Time `json:"processStartTime"`
	HostBootTime     time.Time `json:"hostBootTime"`
	StartedAt        time.Time `json:"startedAt"`
	TunnelPid        int       `json:"tunnelPid,omitempty"`
	TunnelStartTime  time.Time `json:"tunnelStartTime"`
	OwnerPid         int       `json:"ownerPid,omitempty"`
	OwnerStartTime   time.Time `json:"ownerStartTime"`
}

// RunnerOwner identifies one host process. The start time pairs with the
// pid so a recycled pid is never mistaken for the process it replaced.
type RunnerOwner struct {
	Pid       int
	StartTime time.Time
}

// OwnedBy reports whether the record names owner as its owner.
func (s RunnerState) OwnedBy(owner RunnerOwner) bool {
	return s.OwnerPid == owner.Pid && s.OwnerStartTime.Equal(owner.StartTime)
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
// runnerID and owner, so stopping one runner never removes the record of
// a runner launched after it or of a runner another process now owns.
func removeRunnerStateOf(path, runnerID string, owner RunnerOwner) {
	state, err := ReadRunnerState(path)
	if err != nil || state.RunnerID != runnerID || !state.OwnedBy(owner) {
		return
	}
	_ = os.Remove(path)
}
