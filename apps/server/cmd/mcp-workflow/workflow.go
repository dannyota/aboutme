package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	errWorkflowBlocked  = errors.New("workflow_blocked")
	errWorkflowContract = errors.New("workflow_contract_invalid")
	errArguments        = fmt.Errorf("%w: arguments", errWorkflowBlocked)
	errControlRoot      = fmt.Errorf("%w: control root", errWorkflowBlocked)
	errRunRoot          = fmt.Errorf("%w: run root", errWorkflowBlocked)
)

const (
	modeLocal         = "local"
	modeProduction    = "production"
	productionOrigin  = "https://aboutme.vn"
	localOrigin       = "https://localhost:20443"
	browserDirectory  = "browser"
	productionControl = "mcp-workflow"
	developmentRoot   = ".dev"
	workflowTimeout   = 45 * time.Minute
)

type workflowConfig struct {
	Mode        string
	Origin      string
	Control     privateArtifacts
	Run         privateArtifacts
	BrowserRoot string
}

func (c workflowConfig) local() bool { return c.Mode == modeLocal }

func parseRunnerArgs(args []string) (workflowConfig, error) {
	start, err := os.Getwd()
	if err != nil {
		return workflowConfig{}, errWorkflowBlocked
	}
	return parseRunnerArgsFrom(args, start)
}

func parseRunnerArgsFrom(args []string, start string) (workflowConfig, error) {
	if len(args) != 3 || (args[0] != modeLocal && args[0] != modeProduction) {
		return workflowConfig{}, errArguments
	}
	runRoot, browserRoot := args[1], args[2]
	if !filepath.IsAbs(runRoot) || filepath.Clean(runRoot) != runRoot || browserRoot != filepath.Join(runRoot, browserDirectory) {
		return workflowConfig{}, errArguments
	}
	controlRoot := filepath.Dir(runRoot)
	productionShape := filepath.Base(controlRoot) == productionControl && filepath.Base(filepath.Dir(controlRoot)) == developmentRoot
	if args[0] == modeLocal && productionShape {
		return workflowConfig{}, errControlRoot
	}
	if args[0] == modeProduction {
		pinned, pinErr := mainControlRoot(start)
		if pinErr != nil || !sameDirectoryPath(controlRoot, pinned) {
			return workflowConfig{}, errControlRoot
		}
	}
	control, err := openPrivateArtifacts(controlRoot, controlScope)
	if err != nil {
		return workflowConfig{}, errControlRoot
	}
	run, err := openPrivateArtifacts(runRoot, runScope)
	if err != nil || !validPrivateDirectory(browserRoot) {
		return workflowConfig{}, errRunRoot
	}
	config := workflowConfig{Mode: args[0], Origin: localOrigin, Control: control, Run: run, BrowserRoot: browserRoot}
	if args[0] == modeProduction {
		config.Origin = productionOrigin
	}
	if _, originErr := parseOrigin(config.Origin); originErr != nil {
		return workflowConfig{}, errArguments
	}
	return config, nil
}

const (
	maxGitPointerBytes = 4 << 10
	maxRepositoryDepth = 64
)

func mainControlRoot(start string) (string, error) {
	directory := filepath.Clean(start)
	if !filepath.IsAbs(directory) {
		return "", errWorkflowBlocked
	}
	for depth := 0; depth < maxRepositoryDepth; depth++ {
		dotGit := filepath.Join(directory, ".git")
		info, err := os.Lstat(dotGit)
		if err == nil {
			common, commonErr := gitCommonDirectory(dotGit, info)
			if commonErr != nil || filepath.Base(common) != ".git" {
				return "", errWorkflowBlocked
			}
			return filepath.Join(filepath.Dir(common), developmentRoot, productionControl), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", errWorkflowBlocked
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			break
		}
		directory = parent
	}
	return "", errWorkflowBlocked
}

func gitCommonDirectory(dotGit string, info fs.FileInfo) (string, error) {
	if info.IsDir() {
		return dotGit, nil
	}
	if !info.Mode().IsRegular() {
		return "", errWorkflowBlocked
	}
	pointer, err := readSmallFile(dotGit)
	if err != nil {
		return "", err
	}
	gitDirectory, ok := strings.CutPrefix(strings.TrimSpace(string(pointer)), "gitdir: ")
	if !ok || gitDirectory == "" {
		return "", errWorkflowBlocked
	}
	if !filepath.IsAbs(gitDirectory) {
		gitDirectory = filepath.Join(filepath.Dir(dotGit), gitDirectory)
	}
	common, err := readSmallFile(filepath.Join(gitDirectory, "commondir"))
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(string(common))
	if path == "" {
		return "", errWorkflowBlocked
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(gitDirectory, path)
	}
	return filepath.Clean(path), nil
}

func readSmallFile(path string) (data []byte, err error) {
	file, err := os.Open(path) //nolint:gosec // path is a Git metadata file found from the working directory.
	if err != nil {
		return nil, errWorkflowBlocked
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			data, err = nil, errWorkflowBlocked
		}
	}()
	data, err = io.ReadAll(io.LimitReader(file, maxGitPointerBytes+1))
	if err != nil || len(data) > maxGitPointerBytes {
		return nil, errWorkflowBlocked
	}
	return data, nil
}

func sameDirectoryPath(left, right string) bool {
	leftParent, leftErr := filepath.EvalSymlinks(filepath.Dir(left))
	rightParent, rightErr := filepath.EvalSymlinks(filepath.Dir(right))
	return leftErr == nil && rightErr == nil && filepath.Base(left) == filepath.Base(right) && leftParent == rightParent
}

type workflowResult int

const (
	resultCompleted workflowResult = iota + 1
	resultAlreadyComplete
	resultRevocationRecovered
)

func runWorkflow(ctx context.Context, config workflowConfig, deps workflowDeps) (result workflowResult, err error) {
	if !config.Control.validRoot() || !config.Run.validRoot() || !validPrivateDirectory(config.BrowserRoot) {
		return 0, errRunRoot
	}
	if entriesErr := validateRunEntries(config.Run, config.BrowserRoot, config.local()); entriesErr != nil {
		return 0, entriesErr
	}
	lock, err := config.Control.lock()
	if err != nil {
		return 0, errWorkflowBlocked
	}
	defer func() {
		if closeErr := lock.close(); closeErr != nil && err == nil {
			result, err = 0, errWorkflowBlocked
		}
	}()
	_, journalPresent, err := loadJournal(config.Control, config.Origin)
	if err != nil {
		return 0, errRecovery
	}
	if journalPresent {
		if deps.Revocation == nil {
			return 0, errRecovery
		}
		recoverErr := recoverRevocationOnly(ctx, deps.Revocation, config.Control, config.Origin, config.local())
		status := revocationUnconfirmed
		if recoverErr == nil {
			status = revocationRevoked
		}
		if evidenceErr := writeEvidence(config.Run, recoveryEvidence(config.Mode, status)); evidenceErr != nil && recoverErr == nil {
			return 0, evidenceErr
		}
		if recoverErr != nil {
			return 0, recoverErr
		}
		return resultRevocationRecovered, nil
	}
	_, sentinelPresent, err := loadSentinel(config.Control, config.Origin)
	if err != nil {
		return 0, errRecovery
	}
	if sentinelPresent {
		if config.local() {
			return 0, errWorkflowBlocked
		}
		return resultAlreadyComplete, nil
	}
	intent, intentPresent, err := loadCreateIntent(config.Control, config.Origin)
	if err != nil {
		return 0, errRecovery
	}
	if deps.Connect == nil || deps.Observations == nil || deps.Tokens == nil || deps.Revocation == nil ||
		deps.WaitCandidate == nil || deps.NewKey == nil || deps.LocalNow == nil {
		return 0, errWorkflowBlocked
	}
	if runErr := runOwnerWorkflow(ctx, config, deps, intent, intentPresent); runErr != nil {
		return 0, runErr
	}
	return resultCompleted, nil
}
