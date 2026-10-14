package cli

import (
	"dev.atomyx/internal/history"
	"dev.atomyx/internal/storage"
)

// The run catalog must satisfy the history source port structurally,
// because storage cannot import history.
var _ history.Source = (*storage.RunCatalog)(nil)
