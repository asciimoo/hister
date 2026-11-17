package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

const (
	latestReleaseAPIURL = "https://api.github.com/repos/asciimoo/nogon/releases/latest"
	releasesURL         = "https://github.com/asciimoo/nogon/releases"
)

type releaseHTTPClient interface {
	Do(*http.Request) (*http.Response, error)
}

type latestRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

func fetchLatestRelease(ctx context.Context, client releaseHTTPClient, endpoint string) (release latestRelease, retErr error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return latestRelease{}, fmt.Errorf("create release request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", UserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return latestRelease{}, fmt.Errorf("request latest release: %w", err)
	}
	defer func() {
		if err := resp.Body.Close(); err != nil && retErr == nil {
			retErr = fmt.Errorf("close latest release response: %w", err)
		}
	}()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return latestRelease{}, fmt.Errorf("request latest release: HTTP %s", resp.Status)
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&release); err != nil {
		return latestRelease{}, fmt.Errorf("decode latest release: %w", err)
	}
	if release.TagName == "" {
		return latestRelease{}, fmt.Errorf("decode latest release: tag name is missing")
	}
	if release.HTMLURL == "" {
		release.HTMLURL = releasesURL
	}
	return release, nil
}

func parseStableVersion(raw string) ([3]uint64, error) {
	var parsed [3]uint64
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) == 0 {
		return parsed, fmt.Errorf("version is empty")
	}
	value := strings.TrimPrefix(fields[0], "v")
	value, _, _ = strings.Cut(value, "+")
	parts := strings.Split(value, ".")
	if len(parts) != len(parsed) {
		return parsed, fmt.Errorf("version %q must contain major, minor, and patch numbers", raw)
	}
	for i, part := range parts {
		if part == "" || (len(part) > 1 && part[0] == '0') {
			return parsed, fmt.Errorf("version %q contains an invalid number", raw)
		}
		number, err := strconv.ParseUint(part, 10, 64)
		if err != nil {
			return parsed, fmt.Errorf("version %q contains an invalid number: %w", raw, err)
		}
		parsed[i] = number
	}
	return parsed, nil
}

func compareStableVersions(first, second string) (int, error) {
	firstVersion, err := parseStableVersion(first)
	if err != nil {
		return 0, err
	}
	secondVersion, err := parseStableVersion(second)
	if err != nil {
		return 0, err
	}
	for i := range firstVersion {
		if firstVersion[i] < secondVersion[i] {
			return -1, nil
		}
		if firstVersion[i] > secondVersion[i] {
			return 1, nil
		}
	}
	return 0, nil
}

func newCheckUpdateCmd(client releaseHTTPClient, endpoint, currentVersion string) *cobra.Command {
	return &cobra.Command{
		Use:   "check-update",
		Short: "Check whether a new Nogon version is available",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			release, err := fetchLatestRelease(cmd.Context(), client, endpoint)
			if err != nil {
				return err
			}
			comparison, err := compareStableVersions(currentVersion, release.TagName)
			if err != nil {
				return fmt.Errorf("compare Nogon versions: %w", err)
			}
			var message string
			switch comparison {
			case -1:
				message = fmt.Sprintf(
					"A new Nogon version is available: %s\nCurrent version: %s\nRelease: %s\n",
					release.TagName,
					currentVersion,
					release.HTMLURL,
				)
			case 0:
				message = fmt.Sprintf("Nogon %s is up to date.\n", currentVersion)
			case 1:
				message = fmt.Sprintf("Nogon %s is newer than the latest release %s.\n", currentVersion, release.TagName)
			}
			if _, err := fmt.Fprint(cmd.OutOrStdout(), message); err != nil {
				return fmt.Errorf("write update status: %w", err)
			}
			return nil
		},
	}
}

var checkUpdateCmd = newCheckUpdateCmd(
	&http.Client{Timeout: 10 * time.Second},
	latestReleaseAPIURL,
	Version,
)
