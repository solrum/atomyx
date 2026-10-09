package scenario

import "context"

// EntryTarget identifies one run of a suite entry to a host callback.
type EntryTarget struct {
	// EntryIndex is the entry's position in the suite's entry list.
	EntryIndex int
	// EntryPath is the entry's path.
	EntryPath string
}

// EntryEnd is called once with an entry's result, after the entry has
// run and before the next entry starts.
type EntryEnd func(SuiteEntryResult)

// EntryStart is called before an entry runs and returns the function that
// ends the host's work for it, or nil when there is none. The context is
// the suite run's, so a host that must outlive a canceled run detaches
// its own cleanup.
type EntryStart func(ctx context.Context, target EntryTarget) EntryEnd
