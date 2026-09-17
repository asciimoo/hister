// SPDX-License-Identifier: AGPL-3.0-or-later

// Package metrics provides Prometheus instrumentation for Hister.
// All collectors are registered on a dedicated registry so that the
// default process/Go runtime metrics are included only when the
// /metrics endpoint is enabled.
package metrics

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/rs/zerolog/log"
)

// GaugeSource provides live data that the background ticker samples
// periodically to refresh gauge values.
type GaugeSource interface {
	Total() uint64
	DataDir() string
}

// Metrics holds all Prometheus collectors and the background refresh
// goroutine for a single Hister server instance.  Create one with New()
// and call Stop() when the server shuts down.
type Metrics struct {
	QueriesTotal          *prometheus.CounterVec
	SearchDuration        prometheus.Histogram
	DocumentsIndexedTotal *prometheus.CounterVec
	IndexingDuration      prometheus.Histogram
	DatastoreSizeBytes    prometheus.Gauge
	IndexDocumentCount    prometheus.Gauge

	registry    *prometheus.Registry
	gaugeSource GaugeSource
	cancel      context.CancelFunc
}

const gaugeRefreshInterval = 30 * time.Second

// New creates a Metrics instance, registers all collectors on a
// dedicated Prometheus registry, and starts a background ticker that
// refreshes gauge values every 30 seconds.  The ticker is cancelled
// when ctx is done or Stop() is called.
func New(ctx context.Context, src GaugeSource) *Metrics {
	tickCtx, cancel := context.WithCancel(ctx)

	m := &Metrics{
		QueriesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "hister",
			Name:      "queries_total",
			Help:      "Total number of search queries executed.",
		}, []string{"result"}),

		SearchDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "hister",
			Name:      "search_duration_seconds",
			Help:      "Histogram of search query latency in seconds.",
			Buckets:   prometheus.DefBuckets,
		}),

		DocumentsIndexedTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "hister",
			Name:      "documents_indexed_total",
			Help:      "Total number of documents indexed.",
		}, []string{"type"}),

		IndexingDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "hister",
			Name:      "indexing_duration_seconds",
			Help:      "Histogram of single-document indexing latency in seconds.",
			Buckets:   prometheus.DefBuckets,
		}),

		DatastoreSizeBytes: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "hister",
			Name:      "datastore_size_bytes",
			Help:      "Total size of the data directory in bytes.",
		}),

		IndexDocumentCount: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "hister",
			Name:      "index_document_count",
			Help:      "Number of documents currently in the search index.",
		}),

		gaugeSource: src,
		cancel:      cancel,
	}

	m.registry = prometheus.NewRegistry()
	m.registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	m.registry.MustRegister(collectors.NewGoCollector())
	m.registry.MustRegister(
		m.QueriesTotal,
		m.SearchDuration,
		m.DocumentsIndexedTotal,
		m.IndexingDuration,
		m.DatastoreSizeBytes,
		m.IndexDocumentCount,
	)

	go m.runTicker(tickCtx)
	return m
}

// Handler returns an http.Handler that serves the Prometheus metrics.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

// Stop cancels the background gauge-refresh ticker.
func (m *Metrics) Stop() {
	m.cancel()
}

func (m *Metrics) runTicker(ctx context.Context) {
	m.refreshGauges()

	ticker := time.NewTicker(gaugeRefreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.refreshGauges()
		case <-ctx.Done():
			return
		}
	}
}

func (m *Metrics) refreshGauges() {
	if m.gaugeSource == nil {
		return
	}
	m.IndexDocumentCount.Set(float64(m.gaugeSource.Total()))

	dir := m.gaugeSource.DataDir()
	if dir != "" {
		size, err := dirSize(dir)
		if err != nil {
			log.Debug().Err(err).Msg("metrics: failed to compute datastore size")
		} else {
			m.DatastoreSizeBytes.Set(float64(size))
		}
	}
}

func dirSize(path string) (int64, error) {
	var total int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}
