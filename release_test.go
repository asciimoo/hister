package main

import (
	"bytes"
	"encoding/json"
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
		Formats []string
		Name    string `yaml:"name_template"`
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
				if !slices.Equal(archive.Formats, []string{"binary"}) {
					t.Fatal("release downloads must remain standalone binaries")
				}
				// GoReleaser appends the platform extension to the upload name.
				names = append(names, renderReleaseTemplate(t, archive.Name, values)+ext)
			}
			slices.Sort(names)
			suffix := "_0.34.0_" + goos + "_" + goarch + ext
			if want := []string{"hister" + suffix, "nogon" + suffix}; !slices.Equal(names, want) {
				t.Fatalf("download names = %v, want %v", names, want)
			}
		})
	}
	if name := renderReleaseTemplate(t, cfg.Checksum.Name, map[string]string{"Version": "0.34.0"}); name != "hister_0.34.0_checksums.txt" {
		t.Fatalf("legacy checksum download renamed to %q", name)
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
			for _, name := range []string{"nogon_0.34.0_windows_amd64.exe", "hister_0.34.0_windows_amd64.exe", "hister_0.34.0_linux_amd64", "hister_0.34.0_checksums.txt"} {
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
