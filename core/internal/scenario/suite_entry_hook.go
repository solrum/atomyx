package scenario

import "context"

// EntryTarget identifies one attempt of a suite entry to a host callback.
type EntryTarget struct {
	// EntryIndex is the entry's position in the suite's entry list.
	EntryIndex int
	// EntryPath is the entry's path.
	EntryPath string
	// Attempt is the 1-based attempt number. It is 1 for an entry that is
	// not retried.
	Attempt int
}

// EntryEnd is called once with an attempt's result, after the attempt has
// run and before the next attempt or entry starts. Status, Result and
// Error describe that attempt. It returns the files the host kept for the
// attempt, or nil when it kept none.
type EntryEnd func(SuiteEntryResult) []string

// EntryStart is called before each attempt of an entry and returns the
// function that ends the host's work for it, or nil when there is none. The
// context is the suite run's, so a host that must outlive a canceled run
// detaches its own cleanup.
type EntryStart func(ctx context.Context, target EntryTarget) EntryEnd
