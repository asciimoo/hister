package enricher

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// AddFunc is called after successful enrichment to re-index the document.
type AddFunc func(url string, result *Result, existing *ExistingDoc) error

// GetFunc retrieves an existing document's preserved fields by URL.
type GetFunc func(url string) *ExistingDoc

type queueItem struct {
	url    string
	domain string
}

// Manager coordinates registered enrichers and a background worker pool.
type Manager struct {
	enrichers []Enricher
	queue     chan queueItem
	addFn     AddFunc
	getFn     GetFunc
	timeout   time.Duration
	wg        sync.WaitGroup
	done      chan struct{}
}

// New creates a new enricher Manager. Workers begin processing immediately.
// Pass workers=0 to create a manager that only holds enricher registrations
// (useful when enrichment is disabled but enricher metadata is still needed).
func New(workers, timeoutSec int, addFn AddFunc, getFn GetFunc) *Manager {
	if timeoutSec <= 0 {
		timeoutSec = 60
	}
	m := &Manager{
		queue:   make(chan queueItem, 1000),
		addFn:   addFn,
		getFn:   getFn,
		timeout: time.Duration(timeoutSec) * time.Second,
		done:    make(chan struct{}),
	}
	for range workers {
		m.wg.Add(1)
		go m.worker()
	}
	return m
}

// Register adds an enricher to the manager.
func (m *Manager) Register(e Enricher) {
	m.enrichers = append(m.enrichers, e)
}

// Enrichers returns the list of registered enrichers.
func (m *Manager) Enrichers() []Enricher {
	return m.enrichers
}

// Enqueue checks if any registered enricher matches the URL and, if so,
// adds it to the processing queue.
func (m *Manager) Enqueue(url, domain string) {
	for _, e := range m.enrichers {
		if e.Match(url, domain) {
			select {
			case m.queue <- queueItem{url: url, domain: domain}:
			default:
				log.Warn().Str("URL", url).Msg("Enrichment queue full, dropping URL")
			}
			return
		}
	}
}

// Close signals workers to stop and waits for them to finish.
func (m *Manager) Close() {
	close(m.done)
	m.wg.Wait()
}

func (m *Manager) worker() {
	defer m.wg.Done()
	for {
		select {
		case <-m.done:
			return
		case item := <-m.queue:
			m.process(item)
		}
	}
}

func (m *Manager) process(item queueItem) {
	for _, e := range m.enrichers {
		if !e.Match(item.url, item.domain) {
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), m.timeout)
		result, err := e.Enrich(ctx, item.url)
		cancel()

		if err != nil {
			log.Warn().Err(err).
				Str("URL", item.url).
				Str("Enricher", e.Name()).
				Msg("Enrichment failed")
			return
		}

		existing := m.getFn(item.url)
		if err := m.addFn(item.url, result, existing); err != nil {
			log.Error().Err(err).
				Str("URL", item.url).
				Str("Enricher", e.Name()).
				Msg("Failed to index enriched document")
		} else {
			log.Info().
				Str("URL", item.url).
				Str("Enricher", e.Name()).
				Msg("Document enriched")
		}
		return
	}
}
