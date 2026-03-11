package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/ewhauser/jbgo/internal/compatshims"
)

//go:embed manifest.json
var manifestFS embed.FS

type manifest struct {
	GNUVersion    string            `json:"gnu_version"`
	TarballURL    string            `json:"tarball_url"`
	TarballSHA256 string            `json:"tarball_sha256"`
	Utilities     []utilityManifest `json:"utilities"`
	SkipPatterns  []skipPattern     `json:"skip_patterns"`
}

type utilityManifest struct {
	Name     string   `json:"name"`
	Patterns []string `json:"patterns"`
	Skips    []string `json:"skips,omitempty"`
}

type skipPattern struct {
	Pattern string `json:"pattern"`
	Reason  string `json:"reason"`
}

type options struct {
	cacheDir    string
	jbgoBin     string
	utils       string
	tests       string
	setupOnly   bool
	keepWorkdir bool
}

type utilityResult struct {
	Name     string   `json:"name"`
	Tests    []string `json:"tests"`
	Skipped  []string `json:"skipped,omitempty"`
	ExitCode int      `json:"exit_code"`
	Passed   bool     `json:"passed"`
	LogPath  string   `json:"log_path,omitempty"`
	Reason   string   `json:"reason,omitempty"`
}

type runSummary struct {
	GNUVersion string          `json:"gnu_version"`
	WorkDir    string          `json:"work_dir"`
	ResultsDir string          `json:"results_dir"`
	Utilities  []utilityResult `json:"utilities"`
}

func main() {
	ctx := context.Background()
	opts, err := parseOptions()
	if err != nil {
		fatalf("parse options: %v", err)
	}
	manifest, err := loadManifest()
	if err != nil {
		fatalf("load manifest: %v", err)
	}
	if err := run(ctx, manifest, opts); err != nil {
		fatalf("%v", err)
	}
}

func parseOptions() (options, error) {
	var opts options
	fs := flag.NewFlagSet("jbgo-gnu", flag.ContinueOnError)
	fs.StringVar(&opts.cacheDir, "cache-dir", ".cache/gnu", "cache directory for GNU sources and results")
	fs.StringVar(&opts.jbgoBin, "jbgo-bin", "", "path to the jbgo binary under test")
	fs.StringVar(&opts.utils, "utils", strings.TrimSpace(os.Getenv("GNU_UTILS")), "comma or space separated utility list")
	fs.StringVar(&opts.tests, "tests", strings.TrimSpace(os.Getenv("GNU_TESTS")), "comma or newline separated explicit GNU test files")
	fs.BoolVar(&opts.setupOnly, "setup", false, "download and extract the pinned GNU source tree, then exit")
	fs.BoolVar(&opts.keepWorkdir, "keep-workdir", os.Getenv("GNU_KEEP_WORKDIR") == "1", "preserve the per-run workdir")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return options{}, err
	}
	if fs.NArg() != 0 {
		return options{}, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	if !opts.setupOnly && strings.TrimSpace(opts.jbgoBin) == "" {
		return options{}, fmt.Errorf("--jbgo-bin is required unless --setup is used")
	}
	return opts, nil
}

func loadManifest() (*manifest, error) {
	data, err := manifestFS.ReadFile("manifest.json")
	if err != nil {
		return nil, err
	}
	var out manifest
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func run(ctx context.Context, mf *manifest, opts options) error {
	makeBin, err := findMake()
	if err != nil {
		return err
	}
	if err := requireTool("perl"); err != nil {
		return err
	}
	if err := requireCC(); err != nil {
		return err
	}

	cacheDir, err := filepath.Abs(opts.cacheDir)
	if err != nil {
		return err
	}
	sourceDir, err := ensureSourceCache(ctx, mf, cacheDir)
	if err != nil {
		return err
	}
	if opts.setupOnly {
		fmt.Printf("GNU coreutils %s prepared at %s\n", mf.GNUVersion, sourceDir)
		return nil
	}

	jbgoBin, err := filepath.Abs(opts.jbgoBin)
	if err != nil {
		return err
	}
	if err := ensureExecutable(jbgoBin); err != nil {
		return err
	}

	workDir, err := prepareWorkDir(cacheDir, mf.GNUVersion, sourceDir)
	if err != nil {
		return err
	}
	if !opts.keepWorkdir {
		defer func() { _ = os.RemoveAll(workDir) }()
	}

	resultsDir, err := prepareResultsDir(cacheDir)
	if err != nil {
		return fmt.Errorf("create results dir: %w", err)
	}

	if err := configureAndBuild(ctx, makeBin, workDir); err != nil {
		return err
	}

	programs, err := listGNUPrograms(ctx, workDir)
	if err != nil {
		return err
	}
	selectedUtilities, err := selectUtilities(mf, opts.utils)
	if err != nil {
		return err
	}
	runTargets := append([]utilityManifest(nil), selectedUtilities...)
	explicitTests := parseList(opts.tests)
	if len(explicitTests) != 0 {
		runTargets = []utilityManifest{{Name: "explicit-tests"}}
	}
	supportedSet := make(map[string]struct{}, len(selectedUtilities))
	for _, utility := range selectedUtilities {
		supportedSet[utility.Name] = struct{}{}
	}
	if err := prepareProgramDir(workDir, jbgoBin, programs, supportedSet); err != nil {
		return err
	}
	if err := disableCheckRebuild(workDir); err != nil {
		return err
	}

	summary := runSummary{
		GNUVersion: mf.GNUVersion,
		WorkDir:    workDir,
		ResultsDir: resultsDir,
	}
	hadFailure := false
	for _, utility := range runTargets {
		tests, skipped, err := resolveUtilityTests(workDir, utility, mf.SkipPatterns, explicitTests)
		if err != nil {
			return err
		}
		result := utilityResult{
			Name:    utility.Name,
			Tests:   tests,
			Skipped: skipped,
		}
		if len(tests) == 0 {
			result.Reason = "no runnable GNU tests matched after applying skip filters"
			summary.Utilities = append(summary.Utilities, result)
			continue
		}
		logPath := filepath.Join(resultsDir, utility.Name+".log")
		exitCode, err := runMakeCheck(ctx, makeBin, workDir, tests, logPath)
		if err != nil {
			return err
		}
		result.LogPath = logPath
		result.ExitCode = exitCode
		result.Passed = exitCode == 0
		if exitCode != 0 {
			hadFailure = true
		}
		summary.Utilities = append(summary.Utilities, result)
		fmt.Printf("%s: %d tests, exit=%d\n", utility.Name, len(tests), exitCode)
	}

	summaryPath := filepath.Join(resultsDir, "summary.json")
	if err := writeJSON(summaryPath, summary); err != nil {
		return err
	}
	fmt.Printf("results: %s\n", resultsDir)
	if hadFailure {
		return fmt.Errorf("GNU compatibility run failed")
	}
	return nil
}

func findMake() (string, error) {
	for _, candidate := range []string{"gmake", "make"} {
		path, err := exec.LookPath(candidate)
		if err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("could not find make or gmake")
}

func requireTool(name string) error {
	if _, err := exec.LookPath(name); err != nil {
		return fmt.Errorf("missing required tool %q", name)
	}
	return nil
}

func requireCC() error {
	for _, candidate := range []string{os.Getenv("CC"), "cc", "clang", "gcc"} {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		if _, err := exec.LookPath(candidate); err == nil {
			return nil
		}
	}
	return fmt.Errorf("missing required C compiler (tried $CC, cc, clang, gcc)")
}

func ensureSourceCache(ctx context.Context, mf *manifest, cacheDir string) (string, error) {
	downloadsDir := filepath.Join(cacheDir, "downloads")
	sourceRoot := filepath.Join(cacheDir, "src")
	if err := os.MkdirAll(downloadsDir, 0o755); err != nil {
		return "", err
	}
	if err := os.MkdirAll(sourceRoot, 0o755); err != nil {
		return "", err
	}

	tarballPath := filepath.Join(downloadsDir, filepath.Base(mf.TarballURL))
	if _, err := os.Stat(tarballPath); errorsIsNotExist(err) {
		if err := downloadFile(ctx, mf.TarballURL, tarballPath); err != nil {
			return "", err
		}
	}
	if strings.TrimSpace(mf.TarballSHA256) != "" {
		if err := verifySHA256(tarballPath, mf.TarballSHA256); err != nil {
			return "", err
		}
	}

	sourceDir := filepath.Join(sourceRoot, "coreutils-"+mf.GNUVersion)
	if _, err := os.Stat(sourceDir); err == nil {
		return sourceDir, nil
	}
	if err := extractTarGz(tarballPath, sourceRoot); err != nil {
		return "", err
	}
	return sourceDir, nil
}

func prepareWorkDir(cacheDir, version, sourceDir string) (string, error) {
	workRoot := filepath.Join(cacheDir, "work")
	if err := os.MkdirAll(workRoot, 0o755); err != nil {
		return "", err
	}
	workDir, err := os.MkdirTemp(workRoot, "coreutils-"+version+"-")
	if err != nil {
		return "", err
	}
	cmd := exec.Command("cp", "-Rp", sourceDir+"/.", workDir)
	cmd.Stdout = io.Discard
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("copy source tree: %w", err)
	}
	return workDir, nil
}

func configureAndBuild(ctx context.Context, makeBin, workDir string) error {
	configure := exec.CommandContext(ctx, "./configure", "--disable-nls", "--disable-dependency-tracking")
	configure.Dir = workDir
	configure.Stdout = os.Stdout
	configure.Stderr = os.Stderr
	if err := configure.Run(); err != nil {
		return fmt.Errorf("configure GNU coreutils: %w", err)
	}

	makeCmd := exec.CommandContext(ctx, makeBin, "-j", fmt.Sprintf("%d", maxInt(runtime.NumCPU(), 2)))
	makeCmd.Dir = workDir
	makeCmd.Stdout = os.Stdout
	makeCmd.Stderr = os.Stderr
	if err := makeCmd.Run(); err != nil {
		return fmt.Errorf("build GNU coreutils: %w", err)
	}
	return nil
}

func listGNUPrograms(ctx context.Context, workDir string) ([]string, error) {
	cmd := exec.CommandContext(ctx, filepath.Join(workDir, "build-aux", "gen-lists-of-programs.sh"), "--list-progs")
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("list GNU programs: %w", err)
	}
	lines := strings.Fields(string(out))
	sort.Strings(lines)
	return lines, nil
}

func prepareProgramDir(workDir, jbgoBin string, programs []string, supported map[string]struct{}) error {
	srcDir := filepath.Join(workDir, "src")
	supportedNames := make([]string, 0)
	unsupportedNames := make([]string, 0)
	for _, name := range programs {
		path := filepath.Join(srcDir, name)
		if err := os.RemoveAll(path); err != nil {
			return err
		}
		if _, ok := supported[name]; ok {
			supportedNames = append(supportedNames, name)
		} else {
			unsupportedNames = append(unsupportedNames, name)
		}
	}
	if err := compatshims.SymlinkCommands(srcDir, jbgoBin, supportedNames); err != nil {
		return err
	}
	if err := compatshims.WriteUnsupportedStubs(srcDir, unsupportedNames); err != nil {
		return err
	}
	if _, ok := supported["install"]; ok {
		supportedNames = append(supportedNames, "ginstall")
		if err := compatshims.SymlinkCommands(srcDir, jbgoBin, []string{"ginstall"}); err != nil {
			return err
		}
	} else {
		unsupportedNames = append(unsupportedNames, "ginstall")
		if err := compatshims.WriteUnsupportedStubs(srcDir, []string{"ginstall"}); err != nil {
			return err
		}
	}
	if err := installTestRelinkHook(workDir, jbgoBin, supportedNames, unsupportedNames); err != nil {
		return err
	}
	return nil
}

func disableCheckRebuild(workDir string) error {
	makefilePath := filepath.Join(workDir, "Makefile")
	data, err := os.ReadFile(makefilePath)
	if err != nil {
		return err
	}
	updated := strings.Replace(string(data), "check-am: all-am", "check-am:", 1)
	if updated == string(data) {
		return nil
	}
	return os.WriteFile(makefilePath, []byte(updated), 0o644)
}

func installTestRelinkHook(workDir, jbgoBin string, supportedNames, unsupportedNames []string) error {
	hookDir := filepath.Join(workDir, "build-aux", "jbgo-harness")
	if err := os.MkdirAll(hookDir, 0o755); err != nil {
		return err
	}

	if err := writeLines(filepath.Join(hookDir, "supported-programs.txt"), supportedNames); err != nil {
		return err
	}
	if err := writeLines(filepath.Join(hookDir, "unsupported-programs.txt"), unsupportedNames); err != nil {
		return err
	}

	scriptPath := filepath.Join(hookDir, "relink.sh")
	script := fmt.Sprintf(`#!/bin/sh
set -eu

src_dir=$1
script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
jbgo_bin=%s

while IFS= read -r name || [ -n "$name" ]; do
  [ -n "$name" ] || continue
  rm -rf "$src_dir/$name"
  ln -sf "$jbgo_bin" "$src_dir/$name"
done < "$script_dir/supported-programs.txt"

while IFS= read -r name || [ -n "$name" ]; do
  [ -n "$name" ] || continue
  rm -rf "$src_dir/$name"
  cat > "$src_dir/$name" <<'EOF'
#!/bin/sh
printf '%%s: unsupported by jbgo GNU harness\n' "$(basename "$0")" >&2
exit 127
EOF
  chmod 755 "$src_dir/$name"
done < "$script_dir/unsupported-programs.txt"
`, shellSingleQuoteForScript(jbgoBin))
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		return err
	}

	return patchTestsEnvironment(filepath.Join(workDir, "Makefile"))
}

func writeLines(path string, lines []string) error {
	sorted := append([]string(nil), lines...)
	sort.Strings(sorted)
	data := strings.Join(sorted, "\n")
	if data != "" {
		data += "\n"
	}
	return os.WriteFile(path, []byte(data), 0o644)
}

func patchTestsEnvironment(makefilePath string) error {
	data, err := os.ReadFile(makefilePath)
	if err != nil {
		return err
	}
	const needle = "TESTS_ENVIRONMENT = \\\n"
	const injection = "TESTS_ENVIRONMENT = \\\n  $(SHELL) '$(abs_top_builddir)/build-aux/jbgo-harness/relink.sh' '$(abs_top_builddir)/src' || exit $$?; \\\n"
	contents := string(data)
	if strings.Contains(contents, "build-aux/jbgo-harness/relink.sh") {
		return nil
	}
	updated := strings.Replace(contents, needle, injection, 1)
	if updated == contents {
		return fmt.Errorf("patch TESTS_ENVIRONMENT: marker not found")
	}
	return os.WriteFile(makefilePath, []byte(updated), 0o644)
}

func shellSingleQuoteForScript(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func selectUtilities(mf *manifest, raw string) ([]utilityManifest, error) {
	selected := parseList(raw)
	if len(selected) == 0 {
		return append([]utilityManifest(nil), mf.Utilities...), nil
	}
	allowed := make(map[string]utilityManifest, len(mf.Utilities))
	for _, utility := range mf.Utilities {
		allowed[utility.Name] = utility
	}
	out := make([]utilityManifest, 0, len(selected))
	for _, name := range selected {
		utility, ok := allowed[name]
		if !ok {
			return nil, fmt.Errorf("unknown utility %q", name)
		}
		out = append(out, utility)
	}
	return out, nil
}

func resolveUtilityTests(workDir string, utility utilityManifest, globalSkips []skipPattern, explicitTests []string) (testsOut, skippedOut []string, err error) {
	if len(explicitTests) != 0 {
		filtered := make([]string, 0, len(explicitTests))
		for _, test := range explicitTests {
			if skip, _, err := shouldSkipTest(filepath.ToSlash(test), filepath.Join(workDir, test), globalSkips, utility.Skips); err != nil {
				return nil, nil, err
			} else if skip {
				continue
			} else {
				filtered = append(filtered, test)
			}
		}
		sort.Strings(filtered)
		return filtered, nil, nil
	}

	tests := make(map[string]struct{})
	skipped := make([]string, 0)
	for _, pattern := range utility.Patterns {
		matches, err := filepath.Glob(filepath.Join(workDir, pattern))
		if err != nil {
			return nil, nil, err
		}
		sort.Strings(matches)
		for _, match := range matches {
			rel, err := filepath.Rel(workDir, match)
			if err != nil {
				return nil, nil, err
			}
			rel = filepath.ToSlash(rel)
			if skip, reason, err := shouldSkipTest(rel, match, globalSkips, utility.Skips); err != nil {
				return nil, nil, err
			} else if skip {
				skipped = append(skipped, rel+": "+reason)
				continue
			}
			info, err := os.Stat(match)
			if err != nil || info.IsDir() {
				continue
			}
			tests[rel] = struct{}{}
		}
	}
	out := make([]string, 0, len(tests))
	for test := range tests {
		out = append(out, test)
	}
	sort.Strings(out)
	sort.Strings(skipped)
	return out, skipped, nil
}

func shouldSkipTest(rel, path string, globalSkips []skipPattern, utilitySkips []string) (skip bool, reason string, err error) {
	for _, skip := range globalSkips {
		if matched, err := filepath.Match(skip.Pattern, rel); err == nil && matched {
			return true, skip.Reason, nil
		}
	}
	for _, pattern := range utilitySkips {
		if matched, err := filepath.Match(pattern, rel); err == nil && matched {
			return true, "utility-specific skip", nil
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return false, "", err
	}
	contents := string(data)
	switch {
	case strings.Contains(contents, "require_controlling_input_terminal"):
		return true, "controlling TTY tests are skipped in v1", nil
	case strings.Contains(contents, "require_root_"):
		return true, "root-required tests are skipped in v1", nil
	case strings.Contains(contents, "require_selinux_"):
		return true, "SELinux tests are skipped in v1", nil
	case strings.Contains(rel, "help-version"):
		return true, "help/version tests are skipped in v1", nil
	default:
		return false, "", nil
	}
}

func runMakeCheck(ctx context.Context, makeBin, workDir string, tests []string, logPath string) (int, error) {
	args := []string{
		"check",
		"SUBDIRS=.",
		"VERBOSE=no",
		"RUN_EXPENSIVE_TESTS=yes",
		"RUN_VERY_EXPENSIVE_TESTS=yes",
		"srcdir=" + workDir,
		"TESTS=" + strings.Join(tests, " "),
	}
	cmd := exec.CommandContext(ctx, makeBin, args...)
	cmd.Dir = workDir
	output, err := cmd.CombinedOutput()
	if writeErr := os.WriteFile(logPath, output, 0o644); writeErr != nil {
		return 0, writeErr
	}
	if err == nil {
		return 0, nil
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	return 0, err
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func prepareResultsDir(cacheDir string) (string, error) {
	root := filepath.Join(cacheDir, "results")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	return os.MkdirTemp(root, "run-")
}

func downloadFile(ctx context.Context, url, destination string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: unexpected status %s", url, resp.Status)
	}
	tmpPath := destination + ".tmp"
	file, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, resp.Body); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, destination)
}

func verifySHA256(path, expected string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	sum := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(sum, expected) {
		return fmt.Errorf("sha256 mismatch for %s: got %s want %s", path, sum, expected)
	}
	return nil
}

func extractTarGz(archivePath, destination string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	gzr, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer func() { _ = gzr.Close() }()
	tr := tar.NewReader(gzr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target := filepath.Join(destination, header.Name)
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, header.FileInfo().Mode()); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, header.FileInfo().Mode())
			if err != nil {
				return err
			}
			if _, err := io.Copy(file, tr); err != nil {
				_ = file.Close()
				return err
			}
			if err := file.Close(); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(header.Linkname, target); err != nil && !os.IsExist(err) {
				return err
			}
		}
	}
}

func ensureExecutable(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory, want jbgo binary", path)
	}
	if info.Mode()&0o111 == 0 {
		return fmt.Errorf("%s is not executable", path)
	}
	return nil
}

func parseList(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == ' ' || r == '\t'
	})
	out := make([]string, 0, len(fields))
	seen := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		if _, ok := seen[field]; ok {
			continue
		}
		seen[field] = struct{}{}
		out = append(out, field)
	}
	return out
}

func fatalf(format string, args ...any) {
	_, _ = fmt.Fprintf(os.Stderr, "jbgo-gnu: "+format+"\n", args...)
	os.Exit(1)
}

func errorsIsNotExist(err error) bool {
	return err != nil && os.IsNotExist(err)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
