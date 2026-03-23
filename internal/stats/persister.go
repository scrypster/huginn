package stats

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/scrypster/huginn/internal/sqlitedb"
)

const (
	statsFlushInterval = 5 * time.Minute
	statsRetentionDays = 7
	auditRetentionDays = 30
	auditMaxRows       = 10_000
	costChanSize       = 1024
)

// CostEvent is a single cost record enqueued from CostAccumulator.Record().
type CostEvent struct {
	SessionID        string
	CostUSD          float64
	PromptTokens     int
	CompletionTokens int
}

// Persister flushes in-memory stats to SQLite periodically.
//
// Shutdown ordering:
//
//	Close drains the event channel and flushes remaining data to SQLite.
//	Must be called before httpServer.Shutdown() and db.Close().
type Persister struct {
	db      *sqlitedb.DB
	reg     *Registry
	costCh  chan CostEvent
	done    chan struct{}
	wg      sync.WaitGroup
	dropped atomic.Int64
}

// NewPersister creates a Persister and starts its background flush goroutine.
func NewPersister(db *sqlitedb.DB, reg *Registry) *Persister {
	p := &Persister{
		db:     db,
		reg:    reg,
		costCh: make(chan CostEvent, costChanSize),
		done:   make(chan struct{}),
	}
	p.wg.Add(1)
	go p.run()
	return p
}

// EnqueueCost non-blockingly enqueues a cost event for async SQLite persistence.
// If the channel is full the event is dropped and a dropped counter is incremented.
func (p *Persister) EnqueueCost(e CostEvent) {
	select {
	case p.costCh <- e:
	default:
		n := p.dropped.Add(1)
		if p.reg != nil {
			p.reg.Collector().Record("stats.cost_events_dropped", float64(n))
		}
	}
}

// Dropped returns the total number of cost events dropped due to channel backpressure.
func (p *Persister) Dropped() int64 {
	return p.dropped.Load()
}

// Close signals the background goroutine to stop, waits for it to drain the
// cost channel and do a final SQLite flush, then returns.
// Must be called before httpServer.Shutdown() and db.Close().
func (p *Persister) Close() {
	select {
	case <-p.done:
		// already closed
	default:
		close(p.done)
	}
	p.wg.Wait()
}

func (p *Persister) run() {
	defer p.wg.Done()
	ticker := time.NewTicker(statsFlushInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			p.flush()
		case <-p.done:
			p.drainCostChannel()
			p.flushRegistry()
			// Final prune — keep DB tidy on orderly shutdown.
			p.pruneOldData()
			return
		}
	}
}

func (p *Persister) flush() {
	p.flushRegistry()
	p.drainCostChannel()
	p.pruneOldData()
}

func (p *Persister) flushRegistry() {
	if p.reg == nil || p.db == nil {
		return
	}
	snap := p.reg.Snapshot()
	if len(snap.Records) == 0 && len(snap.Histograms) == 0 {
		return
	}
	db := p.db.Write()
	if db == nil {
		return
	}
	ts := time.Now().Unix()
	stmt, err := db.Prepare(`INSERT INTO stats_snapshots (ts, key, kind, value) VALUES (?, ?, ?, ?)`)
	if err != nil {
		return
	}
	defer stmt.Close()
	for _, r := range snap.Records {
		stmt.Exec(ts, r.Metric, "record", r.Value) //nolint:errcheck
	}
	for _, h := range snap.Histograms {
		stmt.Exec(ts, h.Metric, "histogram", h.Value) //nolint:errcheck
	}
}

func (p *Persister) drainCostChannel() {
	if p.db == nil {
		return
	}
	db := p.db.Write()
	if db == nil {
		return
	}
	stmt, err := db.Prepare(`INSERT INTO cost_history (ts, session_id, cost_usd, prompt_tokens, completion_tokens) VALUES (?, ?, ?, ?, ?)`)
	if err != nil {
		return
	}
	defer stmt.Close()
	for {
		select {
		case e := <-p.costCh:
			stmt.Exec(time.Now().Unix(), e.SessionID, e.CostUSD, e.PromptTokens, e.CompletionTokens) //nolint:errcheck
		default:
			return
		}
	}
}

func (p *Persister) pruneOldData() {
	if p.db == nil {
		return
	}
	db := p.db.Write()
	if db == nil {
		return
	}
	statsTs := time.Now().AddDate(0, 0, -statsRetentionDays).Unix()
	auditTs := time.Now().AddDate(0, 0, -auditRetentionDays).Unix()
	db.Exec(`DELETE FROM stats_snapshots WHERE ts < ?`, statsTs)     //nolint:errcheck
	db.Exec(`DELETE FROM cost_history WHERE ts < ?`, statsTs)        //nolint:errcheck
	db.Exec(`DELETE FROM audit_log WHERE ts < ?`, auditTs)           //nolint:errcheck
	// Safety cap for audit_log after time prune (avoids full-scan per insert).
	//nolint:errcheck
	db.Exec(`DELETE FROM audit_log WHERE id IN (
		SELECT id FROM audit_log ORDER BY id ASC
		LIMIT MAX(0, (SELECT count(*) FROM audit_log) - ?)
	)`, auditMaxRows)
}
