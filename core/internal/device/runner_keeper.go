package device

import "sync"

// LeaseAttempt makes one non-blocking attempt to take the lease of the
// device udid. It returns the function that releases the lease, or an
// error when the lease is held by another holder or cannot be taken.
type LeaseAttempt func(udid string) (release func(), err error)

// RunnerKeeper stops the iOS runners its process launched or adopted
// when the process exits. Closing a session never stops a runner, so the
// keeper is the one place that ends a runner for good.
//
// A runner is stopped only after the keeper takes the device's lease with
// one non-blocking attempt, and the lease is released right after. A held
// lease means another process is using the device and the runner, so the
// runner is left running. After taking the lease the keeper reads the
// owner from the record again and leaves a runner whose record names
// another process, because that process adopted it.
type RunnerKeeper struct {
	mu      sync.Mutex
	attempt LeaseAttempt
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

// TakeLeaseWith sets how the keeper takes a device's lease before it
// stops that device's runner, and returns the keeper. A keeper with no
// attempt stops its runners without taking a lease.
func (k *RunnerKeeper) TakeLeaseWith(attempt LeaseAttempt) *RunnerKeeper {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.attempt = attempt
	return k
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

// StopAll stops every kept runner this process still owns and can take the
// lease for, together with
// its tunnel and its record, and returns once they have stopped. It runs
// its work once however many times it is called, and a later call waits
// for the first to finish.
func (k *RunnerKeeper) StopAll() {
	k.stopAll.Do(func() {
		k.mu.Lock()
		attempt := k.attempt
		kept := make([]keptRunner, 0, len(k.runners))
		for _, r := range k.runners {
			kept = append(kept, r)
		}
		k.mu.Unlock()

		var wg sync.WaitGroup
		for _, r := range kept {
			wg.Add(1)
			go func() {
				defer wg.Done()
				stopIfOwned(r, attempt)
			}()
		}
		wg.Wait()
	})
}

// stopIfOwned stops r once the lease of its device is taken, unless the
// record names another owner by then.
func stopIfOwned(r keptRunner, attempt LeaseAttempt) {
	if attempt != nil {
		release, err := attempt(r.launch.udid)
		if err != nil {
			return
		}
		defer release()
	}
	if r.launch.ownedByAnotherProcess() {
		return
	}
	_ = r.stop()
}
