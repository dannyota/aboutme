package main

import (
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	fixedRenderOrigin = "http://127.0.0.1:20030"
	fixedPrintAddress = "127.0.0.1:20082"
)

type config struct {
	RepositoryRoot    string
	BrowserExecutable string
	OutputDirectory   string
	Probe             bool
}

func parseConfig(args []string) (config, error) {
	set := flag.NewFlagSet("render-budget", flag.ContinueOnError)
	set.SetOutput(io.Discard)
	var result config
	set.StringVar(&result.RepositoryRoot, "repository-root", "", "absolute repository root")
	set.StringVar(&result.BrowserExecutable, "chromium-executable", "", "absolute pinned Chromium executable")
	set.StringVar(&result.OutputDirectory, "output-directory", "", "empty output directory below .dev/phase-7")
	set.BoolVar(&result.Probe, "probe", false, "run six cold integration probes without claiming measurement success")
	if err := set.Parse(args); err != nil || set.NArg() != 0 {
		return config{}, errors.New("invalid_arguments")
	}
	if !cleanAbsolute(result.RepositoryRoot) || !cleanAbsolute(result.BrowserExecutable) || !cleanAbsolute(result.OutputDirectory) {
		return config{}, errors.New("invalid_arguments")
	}
	root, err := filepath.EvalSymlinks(result.RepositoryRoot)
	if err != nil || root != result.RepositoryRoot {
		return config{}, errors.New("invalid_repository_root")
	}
	if info, statErr := os.Stat(filepath.Join(root, "go.work")); statErr != nil || !info.Mode().IsRegular() {
		return config{}, errors.New("invalid_repository_root")
	}
	if info, statErr := os.Stat(result.BrowserExecutable); statErr != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return config{}, errors.New("invalid_browser_executable")
	}
	if !pathBelow(filepath.Join(root, ".dev", "phase-7"), result.OutputDirectory) {
		return config{}, errors.New("invalid_output_directory")
	}
	return result, nil
}

func cleanAbsolute(path string) bool {
	return path != "" && filepath.IsAbs(path) && filepath.Clean(path) == path && strings.TrimSpace(path) == path &&
		!strings.ContainsAny(path, "\x00\r\n") && path != string(filepath.Separator)
}

func pathBelow(base, candidate string) bool {
	relative, err := filepath.Rel(base, candidate)
	return err == nil && relative != "." && relative != "" && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func prepareOutputDirectory(repositoryRoot, outputDirectory string) error {
	base := filepath.Join(repositoryRoot, ".dev", "phase-7")
	if !cleanAbsolute(repositoryRoot) || !cleanAbsolute(outputDirectory) || !pathBelow(base, outputDirectory) {
		return errors.New("invalid_output_directory")
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return errors.New("output_directory_failed")
	}
	resolvedBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return errors.New("output_directory_failed")
	}
	relative, _ := filepath.Rel(base, outputDirectory)
	current := base
	for _, part := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, part)
		info, lstatErr := os.Lstat(current)
		if errors.Is(lstatErr, os.ErrNotExist) {
			continue
		}
		if lstatErr != nil || info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && current != outputDirectory) {
			return errors.New("invalid_output_directory")
		}
	}
	if err := os.MkdirAll(outputDirectory, 0o755); err != nil {
		return errors.New("output_directory_failed")
	}
	resolvedOutput, err := filepath.EvalSymlinks(outputDirectory)
	if err != nil || !pathBelow(resolvedBase, resolvedOutput) {
		return errors.New("invalid_output_directory")
	}
	entries, err := os.ReadDir(outputDirectory)
	if err != nil || len(entries) != 0 {
		return errors.New("output_directory_not_empty")
	}
	return nil
}
