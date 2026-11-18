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

func TestSourceBuildCommandName(t *testing.T) {
	cmd := exec.Command("go", "list", "-f", "{{.Target}}", ".")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("resolve the source build command: %v\n%s", err, output)
	}
	want := "nogon"
	if runtime.GOOS == "windows" {
		want += ".exe"
	}
	if got := filepath.Base(strings.TrimSpace(string(output))); got != want {
		t.Fatalf("Go's default executable name = %q, want %q", got, want)
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
			want := []string{"nogon" + suffix}
			if goos == "windows" {
				want = append(want, "nogon_0.34.0_windows_amd64.zip")
			}
			if !slices.Equal(names, want) {
				t.Fatalf("download names = %v, want %v", names, want)
			}
		})
	}
	if name := renderReleaseTemplate(t, cfg.Checksum.Name, map[string]string{"Version": "0.34.0"}); name != "nogon_0.34.0_checksums.txt" {
		t.Fatalf("checksum download name = %q, want Nogon name", name)
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
		{"Nogon command", "nogon_0.99.0_windows_amd64.zip", map[string]string{"nogon.exe": "executable fixture"}, ""},
		{"legacy command only", "nogon_0.99.0_windows_amd64.zip", map[string]string{"hister.exe": "executable fixture"}, "only nogon.exe"},
		{"legacy duplicate", "nogon_0.99.0_windows_amd64.zip", map[string]string{"nogon.exe": "executable fixture", "hister.exe": "executable fixture"}, "only nogon.exe"},
		{"nested command", "nogon_0.99.0_windows_amd64.zip", map[string]string{"nested/nogon.exe": "fixture"}, "only nogon.exe"},
		{"unexpected file", "nogon_0.99.0_windows_amd64.zip", map[string]string{"nogon.exe": "fixture", "../outside.exe": "fixture"}, "only nogon.exe"},
		{"empty archive", "nogon_0.99.0_windows_amd64.zip", nil, "only nogon.exe"},
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
					if manifest["PackageName"] != "Nogon" || manifest["PackageUrl"] != "https://github.com/asciimoo/nogon" || manifest["ManifestType"] != "defaultLocale" {
						t.Fatalf("invalid locale manifest: %v", manifest)
					}
				case ".installer":
					if manifest["InstallerType"] != "zip" || manifest["NestedInstallerType"] != "portable" || manifest["ManifestType"] != "installer" {
						t.Fatalf("invalid archive installer: %v", manifest)
					}
					files := manifest["NestedInstallerFiles"].([]any)
					if len(files) != 1 {
						t.Fatalf("want one portable command, got %v", files)
					}
					file := files[0].(map[string]any)
					if file["RelativeFilePath"] != "nogon.exe" || file["PortableCommandAlias"] != "nogon" {
						t.Errorf("missing Nogon command alias: %v", file)
					}
					if commands := manifest["Commands"].([]any); len(commands) != 1 || commands[0] != "nogon" {
						t.Fatalf("want only the Nogon command, got %v", commands)
					}
					installers := manifest["Installers"].([]any)
					if len(installers) != 1 {
						t.Fatalf("want one installer, got %v", installers)
					}
					installer := installers[0].(map[string]any)
					if installer["Architecture"] != "x64" || installer["InstallerUrl"] != "https://github.com/asciimoo/nogon/releases/download/v0.99.0/"+tc.archive || installer["InstallerSha256"] != fmt.Sprintf("%X", sha256.Sum256(content.Bytes())) {
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
			for _, path := range []string{binary, filepath.Join(dist, "nogon_cgo_"+goos+"_"+goarch+ext)} {
				got, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(got, content) {
					t.Errorf("release copy %s differs or is missing: %v", path, err)
				}
			}
			if _, err := os.Stat(filepath.Join(dist, "hister_cgo_"+goos+"_"+goarch+ext)); !os.IsNotExist(err) {
				t.Errorf("rolling build must not create a Hister binary: %v", err)
			}
		})
	}
}

func TestNixHashUpdateWorkflow(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Nix hash updates run in a Unix shell")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatal(err)
	}
	const oldHash = "sha256-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	const newHash = "sha256-AQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQEBAQE="
	for _, provider := range []string{"github", "forgejo"} {
		var cfg struct {
			Jobs map[string]struct {
				Steps []struct {
					Name string
					Run  string
				}
			}
		}
		readReleaseYAML(t, filepath.Join("."+provider, "workflows", "update-nix-vendor-hash.yml"), &cfg)
		var script string
		for _, step := range cfg.Jobs["update-hashes"].Steps {
			if step.Name == "Update Nix hashes" {
				script = step.Run
			}
		}
		if script == "" {
			t.Fatal("missing Nix hash update step")
		}
		for _, tc := range []struct {
			name        string
			initial     string
			buildError  string
			buildExit   int
			wantError   bool
			verifyError bool
		}{
			{"changed", oldHash, "error: hash mismatch in fixed-output derivation:\n  got:    " + newHash, 1, false, false},
			{"unchanged", newHash, "error: hash mismatch in fixed-output derivation:\n  got:    " + newHash, 1, false, false},
			{"download failure", oldHash, "error: downloading Go modules failed", 1, true, false},
			{"multiple hashes", oldHash, "  got:    " + newHash + "\n  got:    " + oldHash, 1, true, false},
			{"unexpected success", oldHash, "", 0, true, false},
			{"verification failure", oldHash, "error: hash mismatch in fixed-output derivation:\n  got:    " + newHash, 1, true, true},
		} {
			t.Run(provider+"/"+tc.name, func(t *testing.T) {
				root := t.TempDir()
				if err := os.Mkdir(filepath.Join(root, "nix"), 0o700); err != nil {
					t.Fatal(err)
				}
				packageFile := filepath.Join(root, "nix", "package.nix")
				original := "{\n  vendorHash = \"" + tc.initial + "\";\n  otherHash = \"" + tc.initial + "\";\n}\n"
				if err := os.WriteFile(packageFile, []byte(original), 0o600); err != nil {
					t.Fatal(err)
				}
				// Replay Nix's dependency build responses at the workflow boundary.
				// The old updater fails before reaching a dependency build on Nix 2.35.
				stub := `nix() {
  case "$1" in
    run)
      echo 'error: opening file "/nix/store/missing-source": No such file or directory' >&2
      return 1
      ;;
    build)
      for arg in "$@"; do
        if [[ "$arg" == --expr ]]; then
          printf '%s\n' "$NIX_TEST_BUILD_ERROR" >&2
          return "$NIX_TEST_BUILD_EXIT"
        fi
      done
      if [[ " $* " == *" .#nogon "* ]]; then
        cp nix/package.nix verified-package.nix
        if [[ "$NIX_TEST_VERIFY_ERROR" == true ]]; then
          echo 'error: package build failed' >&2
          return 1
        fi
        return 0
      fi
      ;;
  esac
  echo "unexpected Nix invocation: $*" >&2
  return 1
}
`
				cmd := exec.Command(bash, "--noprofile", "--norc", "-e", "-o", "pipefail", "-c", stub+script)
				cmd.Dir = root
				cmd.Env = append(os.Environ(), "NIX_TEST_BUILD_ERROR="+tc.buildError,
					fmt.Sprintf("NIX_TEST_BUILD_EXIT=%d", tc.buildExit), fmt.Sprintf("NIX_TEST_VERIFY_ERROR=%t", tc.verifyError))
				output, err := cmd.CombinedOutput()
				if (err != nil) != tc.wantError {
					t.Fatalf("hash update error = %v, want error %v\n%s", err, tc.wantError, output)
				}
				want := original
				if !tc.wantError || tc.verifyError {
					want = strings.Replace(original, "vendorHash = \""+tc.initial, "vendorHash = \""+newHash, 1)
				}
				got, err := os.ReadFile(packageFile)
				if err != nil || string(got) != want {
					t.Fatalf("package after hash update = %q, want %q: %v", got, want, err)
				}
				verified, err := os.ReadFile(filepath.Join(root, "verified-package.nix"))
				if tc.wantError && !tc.verifyError {
					if !os.IsNotExist(err) {
						t.Fatalf("must stop before verification when hash discovery fails: %v", err)
					}
				} else if err != nil || string(verified) != want {
					t.Fatalf("must build the package with the updated hash: %v\n%s", err, output)
				}
			})
		}
	}
}

func TestReleaseWorkflowRepositoryGuards(t *testing.T) {
	for _, provider := range []string{"github", "forgejo"} {
		for workflow, job := range map[string]string{
			"release": "build", "rolling": "build", "winget-publish": "publish", "update-nix-vendor-hash": "update-hashes",
		} {
			t.Run(provider+"/"+workflow, func(t *testing.T) {
				var cfg struct {
					Jobs map[string]struct{ If string }
				}
				readReleaseYAML(t, filepath.Join("."+provider, "workflows", workflow+".yml"), &cfg)
				if got, want := cfg.Jobs[job].If, provider+".repository == 'asciimoo/nogon'"; got != want {
					t.Errorf("publishing job must allow only the Nogon repository: got %q, want %q", got, want)
				}
			})
		}
	}
}

func TestDockerPublishingImages(t *testing.T) {
	for _, tc := range []struct {
		provider string
		workflow string
		registry string
		steps    int
	}{
		{"github", "docker-publish", "ghcr.io", 3},
		{"github", "docker-release", "ghcr.io", 3},
		{"forgejo", "docker-publish", "codeberg.org", 1},
	} {
		t.Run(tc.provider+"/"+tc.workflow, func(t *testing.T) {
			var cfg struct {
				Env  map[string]string
				Jobs map[string]struct {
					Steps []struct {
						Uses string
						With map[string]string
					}
				}
			}
			readReleaseYAML(t, filepath.Join("."+tc.provider, "workflows", tc.workflow+".yml"), &cfg)
			metadataSteps := 0
			for _, job := range cfg.Jobs {
				for _, step := range job.Steps {
					if !strings.HasPrefix(step.Uses, "docker/metadata-action@") {
						continue
					}
					metadataSteps++
					for _, owner := range []string{"asciimoo", "another-owner"} {
						// Resolve the workflow's image inputs for canonical and fork repositories.
						images := step.With["images"]
						for name, value := range cfg.Env {
							images = strings.ReplaceAll(images, "${{ env."+name+" }}", value)
						}
						images = strings.NewReplacer(
							"${{ "+tc.provider+".repository_owner }}", owner,
							"${{ "+tc.provider+".repository }}", owner+"/nogon",
						).Replace(images)
						got := strings.Fields(images)
						slices.Sort(got)
						want := []string{tc.registry + "/" + owner + "/hister", tc.registry + "/" + owner + "/nogon"}
						if !slices.Equal(got, want) {
							t.Errorf("%s must publish both image names under %s: got %v, want %v", step.With["bake-target"], owner, got, want)
						}
					}
				}
			}
			if metadataSteps != tc.steps {
				t.Errorf("metadata steps = %d, want %d", metadataSteps, tc.steps)
			}
		})
	}
}

func TestForgejoDockerBuildTargets(t *testing.T) {
	type variant struct{ Target, Suffix string }
	var cfg struct {
		Jobs map[string]struct {
			Strategy struct {
				Matrix struct{ Include []variant }
			}
		}
	}
	readReleaseYAML(t, ".forgejo/workflows/docker-publish.yml", &cfg)
	got := cfg.Jobs["build-and-push"].Strategy.Matrix.Include
	want := []variant{{"release", ""}, {"root", "-root"}, {"debug", "-debug"}}
	if !slices.Equal(got, want) {
		t.Errorf("each container variant needs its own build target and tag suffix: got %v, want %v", got, want)
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
				if want := strings.HasPrefix(file, "nogon_cgo_"); uploaded != want {
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
			for _, name := range []string{"nogon_0.34.0_windows_amd64.exe", "hister_0.34.0_windows_amd64.exe", "nogon_0.34.0_windows_amd64.zip", "nogon_0.34.0_linux_amd64", "nogon_0.34.0_checksums.txt"} {
				if matcher.MatchString(name) {
					installers = append(installers, name)
				}
			}
			if want := []string{"nogon_0.34.0_windows_amd64.exe"}; !slices.Equal(installers, want) {
				t.Errorf("WinGet must select the Nogon installer, got %v", installers)
			}
		})
	}
}
