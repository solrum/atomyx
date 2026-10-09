package runfolder

import "path/filepath"

// Directory and file names of the run folder contract. No other
// production file spells these names.
const (
	runsDirName        = "runs"
	versionDirName     = "v1"
	artifactsDirName   = "artifacts"
	reportDirName      = "report"
	screenshotsDirName = "screenshots"

	MetaFileName          = "meta.json"
	StepsFileName         = "steps.jsonl"
	LogFileName           = "log.jsonl"
	ConsoleFileName       = "console.log"
	InterventionsFileName = "interventions.jsonl"

	// MigrateLockFileName is the file under the runs directory that
	// serializes run migration against edits of run records.
	MigrateLockFileName = ".migrate.lock"

	// importTempPrefix names the scratch folders an import builds a run
	// in before renaming it into place. Listings never treat one as a
	// run.
	importTempPrefix = ".import-"

	// LockFileName is the file a live writer holds an exclusive lock on
	// for the whole life of its run.
	LockFileName = "run.lock"

	// IndexFileName is the per-entry record a suite run folder holds.
	IndexFileName = "index.json"

	// JUnitFileName is the JUnit XML report file name.
	JUnitFileName = "junit.xml"

	// VideoNamePrefix begins the name of every recorded video file in
	// a run folder artifacts directory.
	VideoNamePrefix = "video-"
)

// LegacyRecordKeyPrefix is the storage key prefix of the flat run
// records that predate run folders. Only readers use it.
const LegacyRecordKeyPrefix = runsDirName + "/"

// RunsDir returns the directory under root that holds every run, both
// versioned folders and flat records not yet migrated.
func RunsDir(root string) string {
	return filepath.Join(root, runsDirName)
}

// VersionedRunsDir returns the directory under root that holds the
// run folders of this contract version.
func VersionedRunsDir(root string) string {
	return filepath.Join(RunsDir(root), versionDirName)
}

// RunDir returns the folder of one run under root.
func RunDir(root, runID string) string {
	return filepath.Join(VersionedRunsDir(root), runID)
}

// ArtifactsDir returns the directory inside a run folder that holds
// every screenshot and piece of failure evidence.
func ArtifactsDir(runDir string) string {
	return filepath.Join(runDir, artifactsDirName)
}

// ReportDir returns the directory inside a run folder that holds the
// rendered report. Its sibling ArtifactsDir is reachable from it with
// filepath.Rel.
func ReportDir(runDir string) string {
	return filepath.Join(runDir, reportDirName)
}

// LooseScreenshotsDir returns the directory under root that receives a
// screenshot taken while no run is open.
func LooseScreenshotsDir(root string) string {
	return filepath.Join(root, screenshotsDirName)
}

// LockPath returns the lock file of a run folder.
func LockPath(runDir string) string {
	return filepath.Join(runDir, LockFileName)
}

// MigrateLockPath returns the lock file that serializes run migration
// against edits of run records under root.
func MigrateLockPath(root string) string {
	return filepath.Join(RunsDir(root), MigrateLockFileName)
}
