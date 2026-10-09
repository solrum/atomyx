package cli

import (
	"dev.atomyx/internal/config"
)

// videoEnabled reports whether a run records its screen. The --video
// flag turns recording on whatever the config says, and without the flag
// the config's video setting decides. Off is the default.
func videoEnabled(flags map[string]FlagValue, loaded *config.LoadedConfig) bool {
	if flagBool(flags, "--video") {
		return true
	}
	return loaded != nil && loaded.Config.VideoOnFailure()
}
