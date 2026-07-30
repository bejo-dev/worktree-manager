package dependencies

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func touch(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInstallersDetectProjectFiles(t *testing.T) {
	tests := []struct {
		name    string
		files   []string
		wantGo  bool
		wantBun bool
		wantNPM bool
	}{
		{name: "go", files: []string{"go.mod"}, wantGo: true},
		{name: "npm", files: []string{"package.json"}, wantNPM: true},
		{name: "bun lock", files: []string{"package.json", "bun.lock"}, wantBun: true},
		{name: "bun config", files: []string{"package.json", "bunfig.toml"}, wantBun: true},
		{name: "no manifest", files: []string{"README.md"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, file := range tt.files {
				touch(t, dir, file)
			}
			if got := (goInstaller{}).Detect(dir); got != tt.wantGo {
				t.Errorf("Go Detect() = %v, want %v", got, tt.wantGo)
			}
			if got := (bunInstaller{}).Detect(dir); got != tt.wantBun {
				t.Errorf("Bun Detect() = %v, want %v", got, tt.wantBun)
			}
			if got := (npmInstaller{}).Detect(dir); got != tt.wantNPM {
				t.Errorf("npm Detect() = %v, want %v", got, tt.wantNPM)
			}
		})
	}
}

func TestBunDetectsPackageManagerField(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"packageManager":"bun@1.1.0"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if !(bunInstaller{}).Detect(dir) {
		t.Fatal("Bun Detect() = false, want true")
	}
	if (npmInstaller{}).Detect(dir) {
		t.Fatal("npm Detect() = true for a Bun project, want false")
	}
}

func TestInstallerCommands(t *testing.T) {
	tests := []struct {
		name      string
		installer Installer
		files     []string
		command   string
		args      []string
	}{
		{name: "go", installer: goInstaller{}, files: []string{"go.mod"}, command: "go", args: []string{"mod", "download"}},
		{name: "bun", installer: bunInstaller{}, files: []string{"package.json", "bun.lock"}, command: "bun", args: []string{"install", "--frozen-lockfile"}},
		{name: "npm lock", installer: npmInstaller{}, files: []string{"package.json", "package-lock.json"}, command: "npm", args: []string{"ci"}},
		{name: "npm without lock", installer: npmInstaller{}, files: []string{"package.json"}, command: "npm", args: []string{"install", "--no-package-lock"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, file := range tt.files {
				touch(t, dir, file)
			}
			var gotCommand string
			var gotArgs []string
			run := func(projectDir, command string, args ...string) error {
				if projectDir != dir {
					t.Fatalf("project directory = %q, want %q", projectDir, dir)
				}
				gotCommand = command
				gotArgs = args
				return nil
			}
			if err := tt.installer.Install(dir, run); err != nil {
				t.Fatal(err)
			}
			if gotCommand != tt.command || !reflect.DeepEqual(gotArgs, tt.args) {
				t.Fatalf("command = %s %v, want %s %v", gotCommand, gotArgs, tt.command, tt.args)
			}
		})
	}
}
