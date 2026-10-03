package device

import "sync"

// RunnerKeeper stops the iOS runners its process launched when the
// process exits. Closing a session never stops a runner, so the keeper is
// the one place that ends a runner for good. It stops a runner only while
// the runner's record still names this process as owner. A runner whose
// record names another process now serves that process and is left
// running.
type RunnerKeeper struct {
	mu      sync.Mutex
	runners map[string]keptRunner
	stopAll sync.Once
}

type keptRunner struct {
	launch launchRecord
	stop   func() error
}

// NewRunnerKeeper returns a keeper that holds no runner.
func NewRunnerKeeper() *RunnerKeeper {
	return &RunnerKeeper{runners: make(map[string]keptRunner)}
}

func (k *RunnerKeeper) keep(launch launchRecord, stop func() error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.runners[launch.runnerID] = keptRunner{launch: launch, stop: stop}
}

func (k *RunnerKeeper) forget(runnerID string) {
	k.mu.Lock()
	defer k.mu.Unlock()
	delete(k.runners, runnerID)
}

// StopAll stops every kept runner this process still owns, together with
// its tunnel and its record, and returns once they have stopped. It runs
// its work once however many times it is called, and a later call waits
// for the first to finish.
func (k *RunnerKeeper) StopAll() {
	k.stopAll.Do(func() {
		k.mu.Lock()
		kept := make([]keptRunner, 0, len(k.runners))
		for _, r := range k.runners {
			kept = append(kept, r)
		}
		k.mu.Unlock()

		var wg sync.WaitGroup
		for _, r := range kept {
			if r.launch.ownedByAnotherProcess() {
				continue
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = r.stop()
			}()
		}
		wg.Wait()
	})
}
