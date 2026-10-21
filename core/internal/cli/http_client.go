package cli

import (
	"dev.atomyx/internal/config"
	"dev.atomyx/internal/httpclient"
	"dev.atomyx/internal/runner"
)

// newScriptHTTPClient builds the client that httpRequest steps send
// through, from the http policy of the project config. A nil config
// yields an empty allowlist, which refuses every request.
func newScriptHTTPClient(loaded *config.LoadedConfig) runner.HTTPDoer {
	if loaded == nil {
		return httpclient.New(httpclient.Options{})
	}
	return httpclient.New(httpclient.Options{
		AllowHosts: loaded.Config.HTTPAllowHosts(),
		CAFile:     loaded.HTTPCAFile(),
	})
}
