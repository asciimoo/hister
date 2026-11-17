package main

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"testing"
	"text/template"

	"gopkg.in/yaml.v3"
)

type releaseConfig struct {
	Builds []struct {
		Binary string
		Hooks  struct{ Post []string }
	}
	Archives []struct {
		Formats   []string
		Name      string `yaml:"name_template"`
		Overrides []struct {
			GOOS    string `yaml:"goos"`
			Formats []string
		} `yaml:"format_overrides"`
	}
	Checksum struct {
		Name string `yaml:"name_template"`
	}
}

func TestContainerHealthcheckPortAliases(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("container healthchecks run in a Unix shell")
	}
	data, err := os.ReadFile("Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	_, healthcheck, found := strings.Cut(string(data), "HEALTHCHECK ")
	if !found {
		t.Fatal("missing container healthcheck")
	}
	_, command, found := strings.Cut(healthcheck, "CMD ")
	if !found {
		t.Fatal("missing healthcheck command")
	}
	var args []string
	if err := json.Unmarshal([]byte(strings.SplitN(command, "\n", 2)[0]), &args); err != nil {
		t.Fatal(err)
	}
	if len(args) == 0 {
		t.Fatal("empty healthcheck command")
	}
	// Capture the URL requested by the real healthcheck without opening a socket.
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "wget"), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		env  []string
		port string
	}{
		{"defaults", nil, "4433"},
		{"legacy address", []string{"HISTER__SERVER__ADDRESS=0.0.0.0:5001"}, "5001"},
		{"new address", []string{"NOGON__SERVER__ADDRESS=0.0.0.0:5002"}, "5002"},
		{"new address wins", []string{"HISTER__SERVER__ADDRESS=:5001", "NOGON__SERVER__ADDRESS=[::]:5002"}, "5002"},
		{"legacy port", []string{"NOGON__SERVER__ADDRESS=:5002", "HISTER_PORT=5003"}, "5003"},
		{"new port", []string{"HISTER__SERVER__ADDRESS=:5001", "NOGON_PORT=5004"}, "5004"},
		{"new port wins", []string{"NOGON__SERVER__ADDRESS=:5002", "HISTER_PORT=5003", "NOGON_PORT=5004"}, "5004"},
		{"empty new port suppresses legacy", []string{"NOGON__SERVER__ADDRESS=:5002", "HISTER_PORT=5003", "NOGON_PORT="}, "5002"},
		{"empty new port retains legacy address", []string{"HISTER__SERVER__ADDRESS=:5001", "HISTER_PORT=5003", "NOGON_PORT="}, "5001"},
		{"empty ports use default", []string{"HISTER_PORT=5003", "NOGON_PORT="}, "4433"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(args[0], args[1:]...)
			cmd.Env = append(packagingEnvironment(), tc.env...)
			cmd.Env = append(cmd.Env, "PATH="+bin)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("healthcheck failed: %v\n%s", err, output)
			}
			if want := "-qO\n/dev/null\nhttp://localhost:" + tc.port + "/health\n"; string(output) != want {
				t.Errorf("healthcheck request = %q, want %q", output, want)
			}
		})
	}
}

func packagingEnvironment() []string {
	return slices.DeleteFunc(os.Environ(), func(value string) bool {
		return strings.HasPrefix(value, "NOGON_") || strings.HasPrefix(value, "HISTER_") || strings.HasPrefix(value, "COMPOSE_")
	})
}

func TestDatasetScriptCommandAliases(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("dataset scripts require a POSIX shell")
	}
	shell, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}
	dirname, err := exec.LookPath("dirname")
	if err != nil {
		t.Fatal(err)
	}
	script, err := os.ReadFile("scripts/fetch_dataset.sh")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, nogon, hister, want string
		files                     []string
		repoConfig, fails         bool
	}{
		{name: "Nogon override wins", nogon: "custom/new command", hister: "custom/old command", want: "custom/new command", files: []string{"custom/new command", "custom/old command", "work/nogon"}},
		{name: "legacy override", hister: "custom/old command", want: "custom/old command", files: []string{"custom/old command", "work/nogon"}},
		{name: "current Nogon wins", want: "work/nogon", files: []string{"work/nogon", "work/hister", "repo/nogon", "bin/nogon"}},
		{name: "current legacy wins over repository", want: "work/hister", files: []string{"work/hister", "repo/nogon"}},
		{name: "repository Nogon wins", want: "repo/nogon", files: []string{"repo/nogon", "repo/hister", "bin/nogon"}},
		{name: "repository legacy", want: "repo/hister", files: []string{"repo/hister", "bin/nogon"}},
		{name: "PATH Nogon wins", want: "bin/nogon", files: []string{"bin/nogon", "bin/hister"}},
		{name: "PATH legacy", want: "bin/hister", files: []string{"bin/hister"}},
		{name: "repository config selects repository", want: "repo/nogon", repoConfig: true, files: []string{"work/nogon", "repo/nogon"}},
		{name: "invalid override does not fall back", nogon: "custom/missing", hister: "custom/old command", fails: true, files: []string{"custom/old command", "work/nogon"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "dataset fixture")
			for _, name := range []string{"repo/scripts", "work", "bin", "custom"} {
				if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			filename := filepath.Join(dir, "repo/scripts/fetch_dataset.sh")
			if err := os.WriteFile(filename, script, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(dirname, filepath.Join(dir, "bin/dirname")); err != nil {
				t.Fatal(err)
			}
			for _, name := range tc.files {
				if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\nprintf '%s\\000' \"$PWD\" \"$0\" \"$@\"\nprintf '\\n'\n"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			workingDir := filepath.Join(dir, "work")
			if tc.repoConfig {
				workingDir = filepath.Join(dir, "repo")
				if err := os.WriteFile(filepath.Join(workingDir, "config.yml"), nil, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			cmd := exec.Command(shell, filename, "mdn-css")
			cmd.Dir = filepath.Join(dir, "work")
			cmd.Env = append(packagingEnvironment(), "PATH="+filepath.Join(dir, "bin"))
			for key, value := range map[string]string{"NOGON": tc.nogon, "HISTER": tc.hister} {
				if value != "" {
					value = filepath.Join(dir, value)
				}
				cmd.Env = append(cmd.Env, key+"="+value)
			}
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()
			if tc.fails {
				if err == nil || stdout.Len() != 0 {
					t.Fatalf("invalid override ran another executable: %s (%v)", stdout.String(), err)
				}
				return
			}
			if err != nil {
				t.Fatalf("dataset command failed: %v\n%s", err, stderr.String())
			}
			calls := strings.Split(strings.TrimSuffix(stdout.String(), "\n"), "\n")
			if len(calls) != 2 {
				t.Fatalf("expected index and export calls, got %q", stdout.String())
			}
			for i, call := range calls {
				args := strings.Split(strings.TrimSuffix(call, "\x00"), "\x00")
				if len(args) < 3 {
					t.Fatalf("invalid captured command: %q", call)
				}
				executable := args[1]
				if !filepath.IsAbs(executable) {
					executable = filepath.Join(args[0], executable)
				}
				if args[0] != workingDir || filepath.Clean(executable) != filepath.Join(dir, tc.want) {
					t.Errorf("command used directory %q and executable %q, want %q and %q", args[0], executable, workingDir, filepath.Join(dir, tc.want))
				}
				if i == 0 {
					if args[2] != "index" || args[len(args)-1] != "https://developer.mozilla.org/en-US/docs/Web/CSS/Reference/" {
						t.Errorf("incorrect index arguments: %q", args[2:])
					}
				} else if !slices.Equal(args[2:], []string{"export", "mdn-css.json", "label:css"}) {
					t.Errorf("incorrect export arguments: %q", args[2:])
				}
			}
		})
	}
}

func TestSystemdServiceAliasInstallation(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("systemd installation requires Linux")
	}
	if _, err := exec.LookPath("systemctl"); err != nil {
		t.Skip("systemctl is unavailable")
	}
	data, err := os.ReadFile("contrib/systemd/hister.service")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	units := filepath.Join(root, "usr/lib/systemd/system")
	if err := os.MkdirAll(units, 0o755); err != nil {
		t.Fatal(err)
	}
	unit := filepath.Join(units, "hister.service")
	if err := os.WriteFile(unit, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(units, "multi-user.target"), []byte("[Unit]\nDescription=Test target\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// --root only edits the fixture's unit links; it never contacts a service manager.
	cmd := exec.Command("systemctl", "--root", root, "--no-reload", "enable", "hister.service")
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("enable service: %v\n%s", err, output)
	}
	for _, name := range []string{"nogon.service", "multi-user.target.wants/hister.service"} {
		link := filepath.Join(root, "etc/systemd/system", name)
		target, err := os.Readlink(link)
		if err != nil {
			t.Errorf("missing service link %s: %v", name, err)
			continue
		}
		if filepath.IsAbs(target) {
			target = filepath.Join(root, target)
		} else {
			target = filepath.Join(filepath.Dir(link), target)
		}
		if target != unit {
			t.Errorf("%s must resolve to the existing service, got %s", name, target)
		}
	}
	for _, setting := range []string{
		"User=hister", "Group=hister", "StateDirectory=hister",
		"Environment=HISTER_DATA_DIR=%S/hister", "EnvironmentFile=-%E/hister/hister.env",
		"ExecStart=/usr/local/bin/nogon listen", "ProtectSystem=strict", "NoNewPrivileges=true",
	} {
		if !slices.Contains(strings.Split(string(data), "\n"), setting) {
			t.Errorf("missing service compatibility setting %q", setting)
		}
	}
}

func TestComposeImageAliasesPreserveStorage(t *testing.T) {
	if output, err := exec.Command("docker", "compose", "version").CombinedOutput(); err != nil {
		t.Skipf("Docker Compose is unavailable: %v\n%s", err, output)
	}
	for _, tc := range []struct {
		name  string
		env   []string
		image string
		cache string
	}{
		{"defaults", nil, "ghcr.io/asciimoo/hister:latest", "ghcr.io/asciimoo/hister:buildcache"},
		{"legacy", []string{"HISTER_IMAGE=example/hister:old", "HISTER_CACHE_IMAGE=example/hister:cache"}, "example/hister:old", "example/hister:cache"},
		{"new", []string{"NOGON_IMAGE=example/nogon:new", "NOGON_CACHE_IMAGE=example/nogon:cache"}, "example/nogon:new", "example/nogon:cache"},
		{"new wins", []string{"HISTER_IMAGE=example/hister:old", "NOGON_IMAGE=example/nogon:new", "HISTER_CACHE_IMAGE=example/hister:cache", "NOGON_CACHE_IMAGE=example/nogon:cache"}, "example/nogon:new", "example/nogon:cache"},
		{"empty new uses legacy", []string{"NOGON_IMAGE=", "HISTER_IMAGE=example/hister:old", "NOGON_CACHE_IMAGE=", "HISTER_CACHE_IMAGE=example/hister:cache"}, "example/hister:old", "example/hister:cache"},
		{"empty uses default", []string{"NOGON_IMAGE=", "HISTER_IMAGE=", "NOGON_CACHE_IMAGE=", "HISTER_CACHE_IMAGE="}, "ghcr.io/asciimoo/hister:latest", "ghcr.io/asciimoo/hister:buildcache"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Compose config resolves the real file without accessing the Docker daemon.
			// Ignore the developer's .env and override files so no local values leak in.
			cmd := exec.Command("docker", "compose", "-f", "compose.yml", "--env-file", os.DevNull, "--project-name", "nogon-upgrade-test", "config", "--format", "json")
			cmd.Env = append(packagingEnvironment(), tc.env...)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("Compose configuration failed: %v\n%s", err, output)
			}
			var cfg struct {
				Services map[string]struct {
					Image         string
					ContainerName string `json:"container_name"`
					Build         struct {
						CacheFrom []string `json:"cache_from"`
					}
					Volumes []struct{ Type, Source, Target string }
				}
				Volumes map[string]struct{ Name string }
			}
			if err := json.Unmarshal(output, &cfg); err != nil {
				t.Fatal(err)
			}
			service, found := cfg.Services["hister"]
			if !found || len(cfg.Services) != 1 || service.ContainerName != "hister" {
				t.Fatal("Compose must keep the existing service and container identity")
			}
			if service.Image != tc.image {
				t.Errorf("image = %q, want %q", service.Image, tc.image)
			}
			if want := []string{tc.image, "type=registry,ref=" + tc.cache}; !slices.Equal(service.Build.CacheFrom, want) {
				t.Errorf("cache images = %v, want %v", service.Build.CacheFrom, want)
			}
			if len(service.Volumes) != 1 || service.Volumes[0].Type != "volume" || service.Volumes[0].Source != "hister_data" || service.Volumes[0].Target != "/hister/data" {
				t.Fatalf("existing data mount changed: %v", service.Volumes)
			}
			if len(cfg.Volumes) != 1 || cfg.Volumes["hister_data"].Name != "nogon-upgrade-test_hister_data" {
				t.Fatalf("Compose must reuse the project's existing volume: %v", cfg.Volumes)
			}
		})
	}
}

func readReleaseYAML(t *testing.T, path string, value any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal(data, value); err != nil {
		t.Fatal(err)
	}
}

func renderReleaseTemplate(t *testing.T, source string, values map[string]string) string {
	t.Helper()
	tmpl, err := template.New("release").Funcs(template.FuncMap{"dir": filepath.Dir}).Option("missingkey=error").Parse(source)
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, values); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

func TestReleaseDownloadNames(t *testing.T) {
	var cfg releaseConfig
	readReleaseYAML(t, ".goreleaser.yml", &cfg)
	for _, target := range []string{"linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64", "windows/amd64"} {
		t.Run(target, func(t *testing.T) {
			goos, goarch, _ := strings.Cut(target, "/")
			ext := ""
			if goos == "windows" {
				ext = ".exe"
			}
			values := map[string]string{"Version": "0.34.0", "Os": goos, "Arch": goarch, "Arm": "", "Mips": "", "Amd64": "v1", "Binary": cfg.Builds[0].Binary}
			var names []string
			for _, archive := range cfg.Archives {
				formats := archive.Formats
				for _, override := range archive.Overrides {
					if override.GOOS == goos {
						formats = override.Formats
					}
				}
				for _, format := range formats {
					switch format {
					case "binary":
						// GoReleaser appends the platform extension to the upload name.
						names = append(names, renderReleaseTemplate(t, archive.Name, values)+ext)
					case "none":
					default:
						names = append(names, renderReleaseTemplate(t, archive.Name, values)+"."+format)
					}
				}
			}
			slices.Sort(names)
			suffix := "_0.34.0_" + goos + "_" + goarch + ext
			want := []string{"hister" + suffix, "nogon" + suffix}
			if goos == "windows" {
				want = append(want, "nogon_0.34.0_windows_amd64.zip")
			}
			if !slices.Equal(names, want) {
				t.Fatalf("download names = %v, want %v", names, want)
			}
		})
	}
	if name := renderReleaseTemplate(t, cfg.Checksum.Name, map[string]string{"Version": "0.34.0"}); name != "hister_0.34.0_checksums.txt" {
		t.Fatalf("legacy checksum download renamed to %q", name)
	}
}

func TestWinGetManifestPreparation(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("WinGet manifest preparation requires Python 3")
	}
	for _, tc := range []struct {
		name    string
		archive string
		files   map[string]string
		wantErr string
	}{
		{"both commands", "nogon_0.99.0_windows_amd64.zip", map[string]string{"nogon.exe": "executable fixture", "hister.exe": "executable fixture"}, ""},
		{"missing legacy command", "nogon_0.99.0_windows_amd64.zip", map[string]string{"nogon.exe": "executable fixture"}, "nogon.exe and hister.exe"},
		{"different commands", "nogon_0.99.0_windows_amd64.zip", map[string]string{"nogon.exe": "new", "hister.exe": "old"}, "identical"},
		{"nested commands", "nogon_0.99.0_windows_amd64.zip", map[string]string{"nested/nogon.exe": "fixture", "nested/hister.exe": "fixture"}, "nogon.exe and hister.exe"},
		{"unexpected file", "nogon_0.99.0_windows_amd64.zip", map[string]string{"nogon.exe": "fixture", "hister.exe": "fixture", "../outside.exe": "fixture"}, "nogon.exe and hister.exe"},
		{"unreleased snapshot", "nogon_0.99.0-SNAPSHOT_windows_amd64.zip", nil, "stable Windows amd64 release archive"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			archive := filepath.Join(root, tc.archive)
			var content bytes.Buffer
			writer := zip.NewWriter(&content)
			for name, data := range tc.files {
				entry, err := writer.Create(name)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := entry.Write([]byte(data)); err != nil {
					t.Fatal(err)
				}
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(archive, content.Bytes(), 0o600); err != nil {
				t.Fatal(err)
			}
			outputDir := filepath.Join(root, "manifests with spaces")
			cmd := exec.Command(python, "contrib/winget/prepare.py", archive, outputDir)
			output, err := cmd.CombinedOutput()
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(string(output), tc.wantErr) {
					t.Fatalf("want error containing %q, got %v\n%s", tc.wantErr, err, output)
				}
				if _, err := os.Stat(outputDir); !os.IsNotExist(err) {
					t.Fatal("invalid input must not create manifests")
				}
				return
			}
			if err != nil {
				t.Fatalf("manifest preparation failed: %v\n%s", err, output)
			}
			for _, suffix := range []string{"", ".installer", ".locale.en-US"} {
				var manifest map[string]any
				readReleaseYAML(t, filepath.Join(outputDir, "asciimoo.hister"+suffix+".yaml"), &manifest)
				if manifest["PackageIdentifier"] != "asciimoo.hister" || manifest["PackageVersion"] != "0.99.0" {
					t.Fatalf("package identity or version mismatch: %v", manifest)
				}
				switch suffix {
				case "":
					if manifest["ManifestType"] != "version" || manifest["DefaultLocale"] != "en-US" {
						t.Fatalf("invalid version manifest: %v", manifest)
					}
				case ".locale.en-US":
					if manifest["PackageName"] != "Nogon" || manifest["PackageUrl"] != "https://github.com/asciimoo/hister" || manifest["ManifestType"] != "defaultLocale" {
						t.Fatalf("invalid locale manifest: %v", manifest)
					}
				case ".installer":
					if manifest["InstallerType"] != "zip" || manifest["NestedInstallerType"] != "portable" || manifest["ManifestType"] != "installer" {
						t.Fatalf("invalid archive installer: %v", manifest)
					}
					files := manifest["NestedInstallerFiles"].([]any)
					if len(files) != 2 {
						t.Fatalf("want two portable commands, got %v", files)
					}
					for i, command := range []string{"nogon", "hister"} {
						file := files[i].(map[string]any)
						if file["RelativeFilePath"] != command+".exe" || file["PortableCommandAlias"] != command {
							t.Errorf("missing %s command alias: %v", command, file)
						}
					}
					installers := manifest["Installers"].([]any)
					if len(installers) != 1 {
						t.Fatalf("want one installer, got %v", installers)
					}
					installer := installers[0].(map[string]any)
					if installer["Architecture"] != "x64" || installer["InstallerUrl"] != "https://github.com/asciimoo/hister/releases/download/v0.99.0/"+tc.archive || installer["InstallerSha256"] != fmt.Sprintf("%X", sha256.Sum256(content.Bytes())) {
						t.Fatalf("installer does not describe the input archive: %v", installer)
					}
				}
			}
			// Repeating preparation must not overwrite a maintainer's reviewed manifests.
			cmd = exec.Command(python, "contrib/winget/prepare.py", archive, outputDir)
			if output, err := cmd.CombinedOutput(); err == nil || !strings.Contains(string(output), "already exists") {
				t.Fatalf("existing output must be preserved: %v\n%s", err, output)
			}
		})
	}
}

func TestRollingReleaseCopies(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("release hooks run on Linux CI workers")
	}
	var cfg releaseConfig
	readReleaseYAML(t, ".goreleaser-rolling.yml", &cfg)
	for _, target := range []string{"linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64", "windows/amd64"} {
		t.Run(target, func(t *testing.T) {
			goos, goarch, _ := strings.Cut(target, "/")
			ext := ""
			if goos == "windows" {
				ext = ".exe"
			}
			root := t.TempDir()
			dist := filepath.Join(root, "dist with spaces")
			buildDir := filepath.Join(dist, "build_cgo_"+goos+"_"+goarch)
			if err := os.MkdirAll(buildDir, 0o700); err != nil {
				t.Fatal(err)
			}
			binary := filepath.Join(buildDir, cfg.Builds[0].Binary+ext)
			content := []byte("release executable fixture\n")
			if err := os.WriteFile(binary, content, 0o755); err != nil {
				t.Fatal(err)
			}
			values := map[string]string{"Path": binary, "Os": goos, "Arch": goarch, "Arm": "", "Ext": ext, "ProjectName": "nogon"}
			for _, hook := range cfg.Builds[0].Hooks.Post {
				cmd := exec.Command("sh", "-c", renderReleaseTemplate(t, hook, values))
				cmd.Dir = root
				if output, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("release hook failed: %v\n%s", err, output)
				}
			}
			for _, path := range []string{binary, filepath.Join(dist, "nogon_cgo_"+goos+"_"+goarch+ext), filepath.Join(dist, "hister_cgo_"+goos+"_"+goarch+ext)} {
				got, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(got, content) {
					t.Errorf("release copy %s differs or is missing: %v", path, err)
				}
			}
		})
	}
}

func TestReleaseWorkflowDownloads(t *testing.T) {
	for _, provider := range []string{".github", ".forgejo"} {
		t.Run(provider, func(t *testing.T) {
			var rolling struct {
				Jobs map[string]struct {
					Steps []struct {
						Uses string
						With map[string]string
					}
				}
			}
			readReleaseYAML(t, filepath.Join(provider, "workflows/rolling.yml"), &rolling)
			var args, patterns []string
			for _, step := range rolling.Jobs["build"].Steps {
				if strings.HasPrefix(step.Uses, "goreleaser/goreleaser-action@") {
					args = strings.Fields(step.With["args"])
				}
				if strings.HasPrefix(step.Uses, "softprops/action-gh-release@") {
					patterns = strings.Fields(step.With["files"])
				}
			}
			if !slices.Contains(args, ".goreleaser-rolling.yml") {
				t.Errorf("rolling build does not use its configuration: %v", args)
			}
			for _, file := range []string{"nogon_cgo_linux_amd64", "hister_cgo_linux_amd64", "nogon_cgo_windows_amd64.exe", "hister_cgo_windows_amd64.exe", "artifacts.json", "metadata.json", "config.yaml"} {
				var uploaded bool
				for _, pattern := range patterns {
					match, err := path.Match(pattern, "dist/"+file)
					if err != nil {
						t.Fatal(err)
					}
					uploaded = uploaded || match
				}
				if want := strings.Contains(file, "_cgo_"); uploaded != want {
					t.Errorf("upload %s = %t, want %t", file, uploaded, want)
				}
			}

			var winget struct {
				Jobs map[string]struct {
					Steps []struct{ With map[string]string }
				}
			}
			readReleaseYAML(t, filepath.Join(provider, "workflows/winget-publish.yml"), &winget)
			settings := winget.Jobs["publish"].Steps[0].With
			if settings["identifier"] != "asciimoo.hister" {
				t.Fatal("existing WinGet package identity must be retained")
			}
			matcher, err := regexp.Compile(settings["installers-regex"])
			if err != nil {
				t.Fatal(err)
			}
			var installers []string
			for _, name := range []string{"nogon_0.34.0_windows_amd64.exe", "hister_0.34.0_windows_amd64.exe", "nogon_0.34.0_windows_amd64.zip", "hister_0.34.0_linux_amd64", "hister_0.34.0_checksums.txt"} {
				if matcher.MatchString(name) {
					installers = append(installers, name)
				}
			}
			if want := []string{"hister_0.34.0_windows_amd64.exe"}; !slices.Equal(installers, want) {
				t.Errorf("WinGet must select one legacy installer per architecture, got %v", installers)
			}
		})
	}
}
