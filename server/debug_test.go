package server

import (
	"net/http"
	"strings"
	"testing"

	"github.com/asciimoo/nogon/server/testutil"
)

func TestDebugHeapProfileDisabledByDefault(t *testing.T) {
	_, handler := newTokenTestServerWithProfiler(t, false, false)

	rec := testutil.ServeHTTP(t, handler, http.MethodGet, "/debug/pprof/heap?debug=1", nil, map[string]string{
		"X-Access-Token": "secret",
	})

	if strings.Contains(rec.Body.String(), "heap profile:") {
		t.Fatal("heap profile was exposed while app.profiler was off")
	}
}

func TestDebugHeapProfileRequiresToken(t *testing.T) {
	_, handler := newTokenTestServerWithProfiler(t, false, true)

	rec := testutil.ServeHTTP(t, handler, http.MethodGet, "/debug/pprof/heap?debug=1", nil, nil)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("GET /debug/pprof/heap without token status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}

func TestDebugHeapProfileServedWithToken(t *testing.T) {
	_, handler := newTokenTestServerWithProfiler(t, false, true)

	rec := testutil.ServeHTTP(t, handler, http.MethodGet, "/debug/pprof/heap?debug=1", nil, map[string]string{
		"X-Access-Token": "secret",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /debug/pprof/heap status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "heap profile:") {
		t.Fatal("heap profile response missing profile header")
	}
}

func TestDebugPprofIndexServedWithToken(t *testing.T) {
	_, handler := newTokenTestServerWithProfiler(t, false, true)

	rec := testutil.ServeHTTP(t, handler, http.MethodGet, "/debug/pprof/", nil, map[string]string{
		"X-Access-Token": "secret",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /debug/pprof/ status = %d, want %d", rec.Code, http.StatusOK)
	}
	if !strings.Contains(rec.Body.String(), "Types of profiles available") {
		t.Fatal("pprof index response missing profile list")
	}
}

func TestDebugPprofAuxiliaryEndpointsServedWithToken(t *testing.T) {
	_, handler := newTokenTestServerWithProfiler(t, false, true)
	for _, tt := range []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/debug/pprof/cmdline"},
		{http.MethodGet, "/debug/pprof/symbol"},
		{http.MethodPost, "/debug/pprof/symbol"},
	} {
		rec := testutil.ServeHTTP(t, handler, tt.method, tt.path, nil, map[string]string{
			"X-Access-Token": "secret",
		})

		if rec.Code != http.StatusOK {
			t.Fatalf("%s %s status = %d, want %d", tt.method, tt.path, rec.Code, http.StatusOK)
		}
	}
}
