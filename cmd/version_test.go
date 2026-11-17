package cmd

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCompareStableVersions(t *testing.T) {
	tests := []struct {
		first  string
		second string
		want   int
	}{
		{first: "v0.17.0", second: "v0.18.0", want: -1},
		{first: "v1.0.0", second: "v0.99.99", want: 1},
		{first: "v0.17.0 (abcdef0)", second: "0.17.0", want: 0},
		{first: "v0.17.0+build.1", second: "v0.17.0+build.2", want: 0},
	}
	for _, test := range tests {
		name := test.first + "_" + test.second
		t.Run(name, func(t *testing.T) {
			got, err := compareStableVersions(test.first, test.second)
			if err != nil {
				t.Fatal(err)
			}
			if got != test.want {
				t.Fatalf("compareStableVersions() = %d, want %d", got, test.want)
			}
		})
	}
}

func TestCompareStableVersionsRejectsInvalidVersion(t *testing.T) {
	for _, version := range []string{"", "v0.17", "v0.017.0", "latest"} {
		t.Run(version, func(t *testing.T) {
			if _, err := compareStableVersions(version, "v0.17.0"); err == nil {
				t.Fatalf("compareStableVersions(%q) returned no error", version)
			}
		})
	}
}

func TestCheckUpdateCommand(t *testing.T) {
	tests := []struct {
		name           string
		currentVersion string
		latestVersion  string
		want           []string
	}{
		{
			name:           "update available",
			currentVersion: "v0.17.0 (abcdef0)",
			latestVersion:  "v0.18.0",
			want:           []string{"A new Nogon version is available: v0.18.0", "Current version: v0.17.0 (abcdef0)", "Release: https://example.com/v0.18.0"},
		},
		{
			name:           "up to date",
			currentVersion: "v0.18.0",
			latestVersion:  "v0.18.0",
			want:           []string{"Nogon v0.18.0 is up to date."},
		},
		{
			name:           "development version",
			currentVersion: "v0.19.0",
			latestVersion:  "v0.18.0",
			want:           []string{"Nogon v0.19.0 is newer than the latest release v0.18.0."},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("request method = %s, want GET", r.Method)
				}
				if got := r.Header.Get("X-GitHub-Api-Version"); got != "2022-11-28" {
					t.Errorf("API version header = %q", got)
				}
				if _, err := fmt.Fprintf(w, `{"tag_name":%q,"html_url":%q}`, test.latestVersion, "https://example.com/"+test.latestVersion); err != nil {
					t.Errorf("write release response: %v", err)
				}
			}))
			defer server.Close()

			command := newCheckUpdateCmd(server.Client(), server.URL, test.currentVersion)
			var output bytes.Buffer
			command.SetOut(&output)
			if err := command.Execute(); err != nil {
				t.Fatal(err)
			}
			for _, want := range test.want {
				if !strings.Contains(output.String(), want) {
					t.Errorf("command output missing %q:\n%s", want, output.String())
				}
			}
		})
	}
}

func TestCheckUpdateCommandReportsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	command := newCheckUpdateCmd(server.Client(), server.URL, "v0.17.0")
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "HTTP 503 Service Unavailable") {
		t.Fatalf("command error = %v, want HTTP status", err)
	}
}

func TestCheckUpdateCommandRegistration(t *testing.T) {
	command, _, err := rootCmd.Find([]string{"check-update"})
	if err != nil {
		t.Fatal(err)
	}
	if command != checkUpdateCmd {
		t.Fatalf("check-update command = %q, want registered command", command.Name())
	}
}
