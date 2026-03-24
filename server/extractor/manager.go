package extractor

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// AddFunc is called after successful extraction to re-index the document.
type AddFunc func(url string, result *Result, existing *ExistingDoc) error

// GetFunc retrieves an existing document's preserved fields by URL.
type GetFunc func(url string) *ExistingDoc

type queueItem struct {
	url    string
	domain string
}

// Manager coordinates registered extractors and a background worker pool.
// It is a reusable helper that any extractor can leverage for async processing.
type Manager struct {
	extractors []Extractor
	queue      chan queueItem
	addFn      AddFunc
	getFn      GetFunc
	timeout    time.Duration
	wg         sync.WaitGroup
	done       chan struct{}
}

// NewManager creates a new extractor Manager. Workers begin processing immediately.
// Pass workers=0 to create a manager that only holds extractor registrations
// (useful when async extraction is disabled but extractor metadata is still needed).
func NewManager(workers, timeoutSec int, addFn AddFunc, getFn GetFunc) *Manager {
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

// Register adds an extractor to the manager.
func (m *Manager) Register(e Extractor) {
	m.extractors = append(m.extractors, e)
}

// Extractors returns the list of registered extractors.
func (m *Manager) Extractors() []Extractor {
	return m.extractors
}

// Enqueue checks if any registered extractor matches the URL and, if so,
// adds it to the processing queue.
func (m *Manager) Enqueue(url, domain string) {
	for _, e := range m.extractors {
		if e.Match(url, domain) {
			select {
			case m.queue <- queueItem{url: url, domain: domain}:
			default:
				log.Warn().Str("URL", url).Msg("Extraction queue full, dropping URL")
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
	for _, e := range m.extractors {
		if !e.Match(item.url, item.domain) {
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), m.timeout)
		result, err := e.Extract(ctx, &Input{URL: item.url, Domain: item.domain})
		cancel()

		if err != nil {
			log.Warn().Err(err).
				Str("URL", item.url).
				Str("Extractor", e.Name()).
				Msg("Extraction failed")
			return
		}

		existing := m.getFn(item.url)
		if err := m.addFn(item.url, result, existing); err != nil {
			log.Error().Err(err).
				Str("URL", item.url).
				Str("Extractor", e.Name()).
				Msg("Failed to index extracted document")
		} else {
			log.Info().
				Str("URL", item.url).
				Str("Extractor", e.Name()).
				Msg("Document extracted")
		}
		return
	}
}
