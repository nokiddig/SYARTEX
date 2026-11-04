// Package selfupdate implements the one-click update from the ARTEX page: it pulls the new binary
// from a GitHub Release, verifies it, stages it, and atomically swaps it in on the next startup.
//
// Division of labour (see start.sh / start.bat):
//
//	start script = a dumb supervisor loop that only decides, from the exit code, whether to relaunch after the process exits
//	this package = all the error-prone logic (download / SHA256 verification / smoke test / swap / rollback on failure)
//
// The swap lives in Go rather than in the scripts because the sha256 verification and the smoke test would have
// to be written twice, for sh and for bat (sha256sum / shasum / certutil), and this is exactly the step that
// must not go wrong: if a binary that cannot run were swapped in, the supervisor would faithfully relaunch it
// over and over, and the user would have to rescue it by hand on the machine.
//
// A complete upgrade goes through three process starts:
//
//	① the old server receives /api/update/apply -> download and verify -> stage artex.new -> exit 75
//	② the script relaunches the old version -> Bootstrap finds artex.new -> verify + smoke test -> swap -> exit 75
//	③ the script relaunches, now the new version -> Bootstrap records one attempt -> the marker is cleared once startup succeeds
//
// Any failing step falls back to the old version: at ② a failed verification deletes the staged binary and the old version keeps running;
// at ③ if the marker is not cleared 3 times in a row (it crashes right after starting), artex.old is automatically swapped back.
package selfupdate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ExitRestart is the exit code meaning "please have the supervisor relaunch me" (EX_TEMPFAIL). The start script
// reruns immediately when it sees it, without counting it toward crash backoff. 0 means the user stopped normally (the script leaves the loop); everything else is treated as a crash.
const ExitRestart = 75

// maxAttempts is the number of startup attempts allowed after a swap. Each startup of the new version increments the counter by 1,
// and surviving settleDelay clears the marker; crashing maxAttempts times in a row means the new version cannot start at all, so it is rolled back automatically.
const maxAttempts = 3

// Paths holds all the files involved in one upgrade, all placed under the **directory containing the executable**.
// The CWD is deliberately not used: when run as a service the working directory may be / or any path, and using the CWD would put the staged binary
// somewhere else and make the swap logic simply stop working.
type Paths struct {
	Dir     string // directory containing the executable
	Current string // the currently running binary        artex      / artex.exe
	New     string // the staged new version             artex.new  / artex.new.exe
	Sum     string // sha256 of the new version (hex)    artex.new.sha256 / artex.new.exe.sha256
	Old     string // the old version backed up before the swap      artex.old  / artex.old.exe
	Marker  string // upgrade state marker                artex.upgrade.json
}

// ResolvePaths derives all upgrade paths from the current executable.
//
// On Windows .new/.old must also carry the .exe suffix, otherwise the smoke test and the execution after the swap both fail,
// so the suffix is stripped first and then appended, which keeps the naming symmetric on both platforms.
func ResolvePaths() (Paths, error) {
	exe, err := os.Executable()
	if err != nil {
		return Paths{}, fmt.Errorf("locate executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	dir := filepath.Dir(exe)
	name := filepath.Base(exe)
	ext := filepath.Ext(name) // ".exe" on Windows, usually empty on Unix
	stem := strings.TrimSuffix(name, ext)

	join := func(suffix string) string { return filepath.Join(dir, stem+suffix+ext) }
	return Paths{
		Dir:     dir,
		Current: exe,
		New:     join(".new"),
		Sum:     join(".new") + ".sha256",
		Old:     join(".old"),
		Marker:  filepath.Join(dir, stem+".upgrade.json"),
	}, nil
}

// marker records the progress of a swap, used to trigger an automatic rollback if the new version cannot start.
type marker struct {
	From     string `json:"from"`     // version before the upgrade
	To       string `json:"to"`       // target version
	Attempts int    `json:"attempts"` // number of startup attempts since the swap
	StagedAt int64  `json:"staged_at"`
}

func readMarker(path string) (marker, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return marker{}, false
	}
	var m marker
	if json.Unmarshal(b, &m) != nil {
		return marker{}, false
	}
	return m, true
}

func writeMarker(path string, m marker) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// cleanStaged removes the staged binary. A successful swap, a failed verification and a user cancellation all go through it, so that a leftover
// artex.new is not retried at the next startup.
func cleanStaged(p Paths) {
	_ = os.Remove(p.New)
	_ = os.Remove(p.Sum)
}

// CompareVersions compares two version numbers and returns -1/0/1 (a<b / a==b / a>b).
// ok=false means at least one side is not a comparable version (for example "dev" from a local development build, or
// "0.3.7-2-gabc1234-dirty" produced by git describe); the caller should then disable the one-click update,
// otherwise a build under development would be "upgraded" to a release and its uncommitted changes overwritten.
func CompareVersions(a, b string) (int, bool) {
	av, aok := parseVersion(a)
	bv, bok := parseVersion(b)
	if !aok || !bok {
		return 0, false
	}
	for i := range 3 {
		if av[i] != bv[i] {
			if av[i] < bv[i] {
				return -1, true
			}
			return 1, true
		}
	}
	return 0, true
}

// parseVersion parses a version such as "v0.3.7" / "0.3.7" into [3]int.
//
// Only clean three-part versions are accepted: for non-tag builds build.sh uses git describe to produce suffixed versions such as
// "0.3.7-2-gabc1234"; these must be judged not comparable rather than treated as
// 0.3.7 -- otherwise a development build would be misjudged as "already up to date" or be overwritten by a release.
func parseVersion(s string) ([3]int, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	if s == "" {
		return [3]int{}, false
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return [3]int{}, false
	}
	var out [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return [3]int{}, false
		}
		out[i] = n
	}
	return out, true
}

// InDocker reports whether the process is running in a container. In Docker the swap is written to the container's writable layer,
// and recreating the container with `docker compose up -d` goes back to the version baked into the image - this is expected behaviour
// (the user is pulling a new image at that point anyway), but the frontend needs to be able to explain this clearly.
func InDocker() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	b, err := os.ReadFile("/proc/1/cgroup")
	if err != nil {
		return false
	}
	s := string(b)
	return strings.Contains(s, "docker") || strings.Contains(s, "containerd")
}
