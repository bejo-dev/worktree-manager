// Package dependencies detects and installs project dependencies.
//
// Add support for another ecosystem by implementing Installer, then adding an
// instance to the installers slice in Install. Keep detection conservative so
// a repository is not modified unless its project files clearly identify the
// ecosystem.
package dependencies

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CommandRunner runs a dependency-manager command in a project directory.
// It is injectable so installer behavior can be tested without invoking tools.
type CommandRunner func(projectDir, command string, args ...string) error

// Installer detects and installs dependencies for one ecosystem.
type Installer interface {
	Name() string
	Detect(projectDir string) bool
	Install(projectDir string, run CommandRunner) error
}

// Install detects all supported dependency manifests in projectDir and
// installs their dependencies. Multiple installers may apply to one project,
// for example Go and npm in a full-stack repository.
func Install(projectDir string, logw io.Writer) error {
	run := commandRunner(logw)
	for _, installer := range installers {
		if !installer.Detect(projectDir) {
			continue
		}
		if logw != nil {
			fmt.Fprintf(logw, "installing %s dependencies\n", installer.Name())
		}
		if err := installer.Install(projectDir, run); err != nil {
			return fmt.Errorf("install %s dependencies: %w", installer.Name(), err)
		}
	}
	return nil
}

// installers is intentionally ordered. Bun is checked before npm because a
// Bun project can also contain package.json, while npm is the fallback for
// JavaScript projects without Bun markers.
var installers = []Installer{
	goInstaller{},
	bunInstaller{},
	npmInstaller{},
}

type goInstaller struct{}

func (goInstaller) Name() string { return "Go" }

func (goInstaller) Detect(projectDir string) bool {
	return fileExists(filepath.Join(projectDir, "go.mod"))
}

func (goInstaller) Install(projectDir string, run CommandRunner) error {
	return run(projectDir, "go", "mod", "download")
}

type bunInstaller struct{}

func (bunInstaller) Name() string { return "Bun" }

func (bunInstaller) Detect(projectDir string) bool {
	if !fileExists(filepath.Join(projectDir, "package.json")) {
		return false
	}
	return fileExists(filepath.Join(projectDir, "bun.lock")) ||
		fileExists(filepath.Join(projectDir, "bun.lockb")) ||
		fileExists(filepath.Join(projectDir, "bunfig.toml")) ||
		packageManagerIsBun(projectDir)
}

func (bunInstaller) Install(projectDir string, run CommandRunner) error {
	args := []string{"install"}
	if fileExists(filepath.Join(projectDir, "bun.lock")) || fileExists(filepath.Join(projectDir, "bun.lockb")) {
		args = append(args, "--frozen-lockfile")
	}
	return run(projectDir, "bun", args...)
}

type npmInstaller struct{}

func (npmInstaller) Name() string { return "npm" }

func (npmInstaller) Detect(projectDir string) bool {
	if !fileExists(filepath.Join(projectDir, "package.json")) {
		return false
	}
	return !bunInstaller{}.Detect(projectDir)
}

func (npmInstaller) Install(projectDir string, run CommandRunner) error {
	if fileExists(filepath.Join(projectDir, "package-lock.json")) ||
		fileExists(filepath.Join(projectDir, "npm-shrinkwrap.json")) {
		return run(projectDir, "npm", "ci")
	}
	// Avoid creating a tracked package-lock.json as a side effect of acquire.
	return run(projectDir, "npm", "install", "--no-package-lock")
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func packageManagerIsBun(projectDir string) bool {
	data, err := os.ReadFile(filepath.Join(projectDir, "package.json"))
	if err != nil {
		return false
	}
	var manifest struct {
		PackageManager string `json:"packageManager"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return false
	}
	return strings.HasPrefix(manifest.PackageManager, "bun@")
}

func commandRunner(logw io.Writer) CommandRunner {
	return func(projectDir, command string, args ...string) error {
		cmd := exec.Command(command, args...)
		cmd.Dir = projectDir
		cmd.Stdout = logw
		cmd.Stderr = logw
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("run %s: %w", command, err)
		}
		return nil
	}
}
