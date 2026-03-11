package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestParseListDeduplicatesAndSplitsOnCommonSeparators(t *testing.T) {
	got := parseList("ls, cat\nls\tprintf  cat")
	want := []string{"ls", "cat", "printf"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseList() = %#v, want %#v", got, want)
	}
}

func TestResolveUtilityTestsAppliesPatternAndSkipFilters(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "tests/cat/basic.sh", "echo ok\n")
	writeTestFile(t, root, "tests/cat/tty.sh", "require_controlling_input_terminal\n")
	writeTestFile(t, root, "tests/help/help-version.sh", "echo skip\n")

	tests, skipped, err := resolveUtilityTests(root, utilityManifest{
		Name:     "cat",
		Patterns: []string{"tests/cat/*", "tests/help/*"},
	}, []skipPattern{{Pattern: "tests/help/*", Reason: "help/version tests are skipped in v1"}}, nil)
	if err != nil {
		t.Fatalf("resolveUtilityTests() error = %v", err)
	}

	if got, want := tests, []string{"tests/cat/basic.sh"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tests = %#v, want %#v", got, want)
	}
	if len(skipped) != 2 {
		t.Fatalf("skipped = %#v, want two skipped entries", skipped)
	}
}

func TestPrepareResultsDirCreatesParentAndRunDir(t *testing.T) {
	cacheDir := t.TempDir()

	resultsDir, err := prepareResultsDir(cacheDir)
	if err != nil {
		t.Fatalf("prepareResultsDir() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(cacheDir, "results")); err != nil {
		t.Fatalf("Stat(results root) error = %v", err)
	}
	info, err := os.Stat(resultsDir)
	if err != nil {
		t.Fatalf("Stat(results dir) error = %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("results dir %q is not a directory", resultsDir)
	}
}

func TestPrepareWorkDirPreservesFileTimes(t *testing.T) {
	cacheDir := t.TempDir()
	sourceDir := filepath.Join(t.TempDir(), "source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(sourceDir) error = %v", err)
	}
	sourceFile := filepath.Join(sourceDir, "configure")
	if err := os.WriteFile(sourceFile, []byte("generated\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(sourceFile) error = %v", err)
	}
	wantModTime := time.Date(2024, time.January, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(sourceFile, wantModTime, wantModTime); err != nil {
		t.Fatalf("Chtimes(sourceFile) error = %v", err)
	}

	workDir, err := prepareWorkDir(cacheDir, "9.10", sourceDir)
	if err != nil {
		t.Fatalf("prepareWorkDir() error = %v", err)
	}

	info, err := os.Stat(filepath.Join(workDir, "configure"))
	if err != nil {
		t.Fatalf("Stat(copied configure) error = %v", err)
	}
	if !info.ModTime().Equal(wantModTime) {
		t.Fatalf("copied mod time = %v, want %v", info.ModTime(), wantModTime)
	}
}

func writeTestFile(t *testing.T, root, rel, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", path, err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("WriteFile(%q) error = %v", path, err)
	}
}
