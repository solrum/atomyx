package cli

import (
	"context"
	"io"

	"dev.atomyx/internal/config"
	"dev.atomyx/internal/device"
	"dev.atomyx/internal/netcapture"
	"dev.atomyx/internal/runfolder"
	"dev.atomyx/internal/runner"
	"dev.atomyx/internal/scenario"
	"dev.atomyx/internal/stepstream"
)

// suiteSettings is what a suite run takes from its flags and the project
// config, resolved once so the in-process suite and a sharding worker read
// them the same way.
type suiteSettings struct {
	loaded              *config.LoadedConfig
	environment         *config.ResolvedEnvironment
	cliPreconditions    runner.PreconditionsOverride
	configPreconditions runner.PreconditionsOverride
	policy              suitePolicy
	output              runOutput
	externalEnv         map[string]string
	committedEnv        map[string]bool
	cwd                 string
	testRoots           []string
	label               string
}

// resolveSuiteSettings reads everything a suite run needs from flags and the
// project config, except which entries to run. A returned error is a usage
// error. The working directory is read separately, so a failure there is
// not mistaken for one.
func resolveSuiteSettings(flags map[string]FlagValue, positional *string) (suiteSettings, error) {
	loaded, environment, err := resolveEnvironment(flags)
	if err != nil {
		return suiteSettings{}, err
	}
	cliPreconditions, err := resolveCliPreconditionsFlag(flags)
	if err != nil {
		return suiteSettings{}, err
	}
	policy, err := resolveSuitePolicy(positional, flags, loaded)
	if err != nil {
		return suiteSettings{}, err
	}
	var configReport *config.Report
	if loaded != nil {
		configReport = loaded.Config.Report
	}
	output, err := resolveReportOptions(flags, configReport)
	if err != nil {
		return suiteSettings{}, err
	}
	externalEnv, committedEnv, err := buildExternalEnv(flags, environment)
	if err != nil {
		return suiteSettings{}, err
	}
	var testRoots []string
	if loaded != nil && loaded.Config.TestRoots != nil {
		testRoots = *loaded.Config.TestRoots
	}
	label := "suite"
	if positional != nil {
		label = *positional
	}
	return suiteSettings{
		loaded:              loaded,
		environment:         environment,
		cliPreconditions:    cliPreconditions,
		configPreconditions: configPreconditionsOverride(loaded),
		policy:              policy,
		output:              output,
		externalEnv:         externalEnv,
		committedEnv:        committedEnv,
		testRoots:           testRoots,
		label:               label,
	}, nil
}

// suiteHost is a suite runner wired to one device, the run folder it
// writes, and the context that ends when the device is lost.
type suiteHost struct {
	opened *deviceSession
	runCtx context.Context
	cancel context.CancelCauseFunc
	record *runRecord
	runner *scenario.SuiteRunner
	config suiteSettings
}

// startSuiteHost acquires the device desc, opens the run folder, and builds
// the suite runner that drives them. parentRunID, when set, names the
// sharding run that started this one. The caller closes the host with
// close.
func startSuiteHost(ctx context.Context, deps Deps, cfg suiteSettings, proxyFlag, parentRunID string, desc device.Descriptor, videoOn bool) (*suiteHost, error) {
	opened, err := borrowDevice(ctx, deps, "atomyx suite", "atomyx suite", desc)
	if err != nil {
		return nil, err
	}
	runCtx, cancelRun := context.WithCancelCause(ctx)
	abandon := func() { //nolint:contextcheck // teardown is bounded by its own grace timer, not by a call context
		cancelRun(nil)
		opened.Close()
	}
	opened.cancelOnLost(cancelRun)

	networkCapture, err := netcapture.New(parseCaptureFlag(proxyFlag))
	if err != nil {
		abandon()
		return nil, err
	}
	// The run folder is created before the suite runs. Its id
	// correlates every entry's live step trace and names the folder
	// holding the suite's report.
	record, err := startRunRecord(cfg.output, runIdentity{
		Kind:        runfolder.KindSuite,
		Label:       cfg.label,
		Device:      desc,
		ParentRunID: parentRunID,
	})
	if err != nil {
		abandon()
		return nil, err
	}
	opened.recordRun(record)
	record.secrets.AddExternal(cfg.externalEnv, cfg.committedEnv)
	addEnvRefValues(record.secrets, cfg.environment)
	runLog := record.maskedLogger(deps.logger())

	videos := newSuiteVideos(deps.videoLifecycle(videoOn), desc, record.writer)
	suiteRunner := scenario.NewSuiteRunner(scenario.SuiteRunnerDeps{
		Orchestra:         opened.Built().Orchestra,
		Clock:             deps.clock(),
		Logger:            runLog,
		RunID:             record.ID(),
		NetworkCapture:    networkCapture,
		Commands:          runner.DefaultCommands(),
		CreateLoadScript:  fsScenarioScriptLoader,
		ExternalEnv:       cfg.externalEnv,
		ExternalCommitted: cfg.committedEnv,
		SecretSink:        record.secrets,
		ChildArtifacts:    newArtifactSequence(runfolder.ArtifactsDir(record.Dir())).forTarget,
		OnStep:            stepstream.NewSink(record.writer, runLog, stepstream.WithPublisher(opened.PublishStep), stepstream.WithMask(record.secrets.String)),
		OnEntryStart:      videos.start,
	})
	return &suiteHost{opened: opened, runCtx: runCtx, cancel: cancelRun, record: record, runner: suiteRunner, config: cfg}, nil
}

// close finalizes the run folder, then releases the device.
func (h *suiteHost) close(ctx context.Context, stderr io.Writer) {
	h.record.end(ctx, stderr)
	h.cancel(nil)
	h.opened.Close() //nolint:contextcheck // teardown is bounded by its own grace timer, not by a call context
}

// runOptions returns the options of one Run call. offset is the position
// of the call's first entry in the host's whole entry list, so the
// artifact and video names of entries run in separate calls never collide.
func (h *suiteHost) runOptions(suiteName *string, offset int) *scenario.SuiteRunOptions {
	return &scenario.SuiteRunOptions{
		SuiteName: suiteName,
		ResolvePreconditions: func(flowLevel *string) runner.PreconditionsOverride {
			scenarioOverride := runner.PreconditionsNoOverride
			if flowLevel != nil {
				scenarioOverride = runner.PreconditionsOverride(*flowLevel)
			}
			return resolvePreconditionsOverride(h.config.cliPreconditions, scenarioOverride, h.config.configPreconditions)
		},
		Retries:     h.config.policy.Retries,
		Quarantined: quarantineMatcher(h.config.policy.Quarantine, h.config.cwd, h.config.testRoots),
		EntryOffset: offset,
	}
}
