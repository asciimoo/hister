package server

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"

	"github.com/asciimoo/hister/server/model"
	"github.com/asciimoo/hister/server/testutil"
)

type researchFixture struct {
	t       *testing.T
	handler http.Handler
}

func newResearchFixture(t *testing.T) *researchFixture {
	t.Helper()
	cfg := testutil.InitModel(t)
	cfg.App.AccessToken = "secret"
	sessionStore = newSessionStore([]byte(strings.Repeat("x", 32)), cfg.BaseURL(""), sessionMaxAge)
	idx := newServerTestIndexer(t, cfg)
	return &researchFixture{t: t, handler: registerEndpoints(cfg, idx)}
}

func (f *researchFixture) headers(extra map[string]string) map[string]string {
	h := map[string]string{"Authorization": "Bearer secret", "Origin": "hister://", "Accept": "application/json"}
	for k, v := range extra {
		h[k] = v
	}
	return h
}

func (f *researchFixture) createJob(name string) model.ResearchJob {
	f.t.Helper()
	body := fmt.Sprintf(`{"name":%q}`, name)
	r := testutil.ServeHTTP(f.t, f.handler, http.MethodPost, "/api/research/jobs", strings.NewReader(body), f.headers(map[string]string{"Content-Type": "application/json"}))
	if r.Code != http.StatusCreated {
		f.t.Fatalf("create job: %d %s", r.Code, r.Body)
	}
	var job model.ResearchJob
	if err := json.Unmarshal(r.Body.Bytes(), &job); err != nil {
		f.t.Fatal(err)
	}
	return job
}

type uploadFile struct{ path, name, content string }

func (f *researchFixture) upload(jobID uint, files ...uploadFile) (int, []model.ResearchSource) {
	f.t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for _, file := range files {
		_ = w.WriteField("paths", file.path)
	}
	for _, file := range files {
		part, err := w.CreateFormFile("files", file.name)
		if err != nil {
			f.t.Fatal(err)
		}
		_, _ = part.Write([]byte(file.content))
	}
	_ = w.Close()
	r := testutil.ServeHTTP(f.t, f.handler, http.MethodPost, fmt.Sprintf("/api/research/jobs/%d/files", jobID), &buf, f.headers(map[string]string{"Content-Type": w.FormDataContentType()}))
	var out struct {
		Sources []model.ResearchSource `json:"sources"`
	}
	if r.Code == http.StatusCreated {
		if err := json.Unmarshal(r.Body.Bytes(), &out); err != nil {
			f.t.Fatal(err)
		}
	}
	return r.Code, out.Sources
}

func (f *researchFixture) search(jobID uint, q string) []researchSearchHit {
	f.t.Helper()
	r := testutil.ServeHTTP(f.t, f.handler, http.MethodGet, fmt.Sprintf("/api/research/jobs/%d/search?q=%s", jobID, q), nil, f.headers(nil))
	if r.Code != http.StatusOK {
		f.t.Fatalf("search: %d %s", r.Code, r.Body)
	}
	var out struct {
		Results []researchSearchHit `json:"results"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &out); err != nil {
		f.t.Fatal(err)
	}
	return out.Results
}

func sha(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func TestResearchJobIngestDuplicatesAndScopedSearch(t *testing.T) {
	f := newResearchFixture(t)
	a := f.createJob("Grace papers")
	b := f.createJob("Entropy papers")

	graceText := "# Grace\n\nGrace acts as an external negentropic source term in the moral ledger."
	code, sources := f.upload(a.ID,
		uploadFile{"papers/grace.md", "grace.md", graceText},
		uploadFile{"papers/copy/grace-copy.md", "grace-copy.md", graceText},
		uploadFile{"notes.txt", "notes.txt", "Coherence and the quantum observer, a working note."},
	)
	if code != http.StatusCreated || len(sources) != 3 {
		t.Fatalf("upload: %d %+v", code, sources)
	}
	first, dup, notes := sources[0], sources[1], sources[2]
	if first.SHA256 != sha(graceText) || first.Size != int64(len(graceText)) {
		t.Fatalf("receipt hash/size wrong: %+v", first)
	}
	if first.RelativePath != "papers/grace.md" || first.Status != model.ResearchSourceIndexed || first.DuplicateKind != "" {
		t.Fatalf("first receipt: %+v", first)
	}
	if dup.DuplicateKind != model.DuplicateInJob || dup.DuplicateOfID == nil || *dup.DuplicateOfID != first.ID || dup.ArtifactID != first.ArtifactID {
		t.Fatalf("in-job duplicate not detected: %+v", dup)
	}
	if notes.Status != model.ResearchSourceIndexed {
		t.Fatalf("notes not indexed: %+v", notes)
	}

	// Same bytes in another job: new receipt, flagged, shares the artifact.
	_, other := f.upload(b.ID, uploadFile{"grace.md", "grace.md", graceText}, uploadFile{"heat.txt", "heat.txt", "Thermodynamic heat death and judgment."})
	if other[0].DuplicateKind != model.DuplicateOtherJob || other[0].ArtifactID != first.ArtifactID || other[0].JobID != b.ID {
		t.Fatalf("cross-job duplicate not detected: %+v", other[0])
	}

	// Search is scoped to the job.
	if hits := f.search(a.ID, "negentropic"); len(hits) != 1 || hits[0].ArtifactID != first.ArtifactID || !strings.Contains(hits[0].Snippet, "external negentropic source") || strings.Contains(hits[0].Snippet, "\x1b") {
		t.Fatalf("job A grace search: %+v", hits)
	}
	if hits := f.search(a.ID, "thermodynamic"); len(hits) != 0 {
		t.Fatalf("job A must not see job B documents: %+v", hits)
	}
	if hits := f.search(b.ID, "thermodynamic"); len(hits) != 1 {
		t.Fatalf("job B heat search: %+v", hits)
	}
	if hits := f.search(b.ID, "negentropic"); len(hits) != 1 {
		t.Fatalf("job B shares the grace artifact: %+v", hits)
	}

	// Job detail lists receipts; list shows counts.
	r := testutil.ServeHTTP(t, f.handler, http.MethodGet, "/api/research/jobs", nil, f.headers(nil))
	var list struct {
		Jobs []model.ResearchJobSummary `json:"jobs"`
	}
	_ = json.Unmarshal(r.Body.Bytes(), &list)
	if len(list.Jobs) != 2 || list.Jobs[1].ID != a.ID || list.Jobs[1].Sources != 3 || list.Jobs[1].Duplicates != 1 || list.Jobs[1].Indexed != 3 {
		t.Fatalf("job list counts: %+v", list.Jobs)
	}
}

func TestResearchUploadRecordsFailures(t *testing.T) {
	f := newResearchFixture(t)
	job := f.createJob("Failures")
	_, sources := f.upload(job.ID, uploadFile{"empty.txt", "empty.txt", "   "})
	if len(sources) != 1 || sources[0].Status != model.ResearchSourceFailed || sources[0].Error == "" || sources[0].SHA256 != sha("   ") {
		t.Fatalf("failure must keep receipt with error: %+v", sources)
	}
	// Retrying the same bytes after a failure tries to index again rather
	// than reporting the duplicate as searchable.
	_, again := f.upload(job.ID, uploadFile{"empty.txt", "empty.txt", "   "})
	if again[0].Status != model.ResearchSourceFailed || again[0].DuplicateKind != model.DuplicateInJob {
		t.Fatalf("retry of unindexed duplicate: %+v", again[0])
	}
}

func TestResearchValidation(t *testing.T) {
	f := newResearchFixture(t)
	jsonH := f.headers(map[string]string{"Content-Type": "application/json"})
	if r := testutil.ServeHTTP(t, f.handler, http.MethodPost, "/api/research/jobs", strings.NewReader(`{"name":"  "}`), jsonH); r.Code != http.StatusBadRequest {
		t.Fatalf("blank name: %d", r.Code)
	}
	for _, p := range []string{"/api/research/jobs/999", "/api/research/jobs/999/search?q=x"} {
		if r := testutil.ServeHTTP(t, f.handler, http.MethodGet, p, nil, f.headers(nil)); r.Code != http.StatusNotFound {
			t.Fatalf("%s: %d", p, r.Code)
		}
	}
	if r := testutil.ServeHTTP(t, f.handler, http.MethodGet, "/api/research/jobs/abc", nil, f.headers(nil)); r.Code != http.StatusBadRequest {
		t.Fatalf("bad id: %d", r.Code)
	}
	job := f.createJob("Empty upload")
	if code, _ := f.upload(job.ID); code != http.StatusBadRequest {
		t.Fatalf("empty upload: %d", code)
	}
	if got := cleanRelativePath(`..\..\etc/passwd`); got != "etc/passwd" {
		t.Fatalf("path cleaning: %q", got)
	}
}

func TestResearchJobsAreUserScoped(t *testing.T) {
	f := newResearchFixture(t)
	job := f.createJob("Mine")
	other, err := model.CreateResearchJob(job.UserID+1, "Someone else", "")
	if err != nil {
		t.Fatal(err)
	}
	if r := testutil.ServeHTTP(t, f.handler, http.MethodGet, fmt.Sprintf("/api/research/jobs/%d", other.ID), nil, f.headers(nil)); r.Code != http.StatusNotFound {
		t.Fatalf("other user's job visible: %d", r.Code)
	}
	if code, _ := f.upload(other.ID, uploadFile{"x.txt", "x.txt", "intrusion"}); code != http.StatusNotFound {
		t.Fatalf("upload into other user's job: %d", code)
	}
}

func TestResearchPageServed(t *testing.T) {
	f := newResearchFixture(t)
	r := testutil.ServeHTTP(t, f.handler, http.MethodGet, "/research", nil, map[string]string{"Authorization": "Bearer secret"})
	if r.Code != http.StatusOK || !strings.Contains(r.Body.String(), "Research jobs") || !strings.Contains(r.Body.String(), `<script nonce="`) {
		t.Fatalf("research page: %d", r.Code)
	}
}
