// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package atepg

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/agent-substrate/substrate/cmd/ateapi/internal/store"
)

func TestAcquireLease_CleansExpiredLeases(t *testing.T) {
	s := setupPostgresPersistence(t)
	ctx := context.Background()
	if _, err := s.pool.Exec(ctx, `
		INSERT INTO leases (key, token, expires_at) VALUES
		('expired', 'old', clock_timestamp() - interval '1 minute'),
		('active', 'live', clock_timestamp() + interval '1 hour')`); err != nil {
		t.Fatalf("seeding leases: %v", err)
	}
	lease, err := s.AcquireLease(ctx, "new", "test")
	if err != nil {
		t.Fatalf("AcquireLease: %v", err)
	}
	defer lease.Close()

	var expired, active int
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM leases WHERE key = 'expired'`).Scan(&expired); err != nil {
		t.Fatalf("counting expired lease: %v", err)
	}
	if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM leases WHERE key = 'active'`).Scan(&active); err != nil {
		t.Fatalf("counting active lease: %v", err)
	}
	if expired != 0 || active != 1 {
		t.Errorf("lease counts = expired:%d active:%d, want 0 and 1", expired, active)
	}
}

func TestAcquireLease_RecordsHolder(t *testing.T) {
	s := setupPostgresPersistence(t)
	ctx := context.Background()
	before := time.Now()
	lease, err := s.AcquireLease(ctx, "actor-1", "resume")
	if err != nil {
		t.Fatalf("AcquireLease: %v", err)
	}
	defer lease.Close()

	var holder string
	var acquiredAt time.Time
	if err := s.pool.QueryRow(ctx, `SELECT holder, acquired_at FROM leases WHERE key = 'actor-1'`).Scan(&holder, &acquiredAt); err != nil {
		t.Fatalf("reading the lease row: %v", err)
	}
	if holder != "resume" {
		t.Errorf("holder = %q, want resume", holder)
	}
	if acquiredAt.Before(before.Add(-time.Second)) || acquiredAt.After(time.Now().Add(time.Second)) {
		t.Errorf("acquired_at = %v, want about %v", acquiredAt, before)
	}

	// A row an older release wrote carries the defaults: no holder, seen now.
	if _, err := s.pool.Exec(ctx, `INSERT INTO leases (key, token, expires_at) VALUES ('legacy', 'old', clock_timestamp() + interval '1 hour')`); err != nil {
		t.Fatalf("seeding a legacy lease: %v", err)
	}
	_, err = s.AcquireLease(ctx, "legacy", "pause")
	var held *store.LeaseConflictError
	if !errors.As(err, &held) || held.Holder != "" || held.Since.IsZero() {
		t.Errorf("AcquireLease on a legacy row = %v, want a conflict with an empty holder and the time it was seen", err)
	}
}

func TestAcquireLease_ExpiresAfterHolderStops(t *testing.T) {
	s := setupPostgresPersistence(t)
	s.leaseTTL = 200 * time.Millisecond
	holderCtx, cancelHolder := context.WithCancel(context.Background())
	lease, err := s.AcquireLease(holderCtx, "test-lease", "holder")
	if err != nil {
		t.Fatalf("AcquireLease failed: %v", err)
	}
	cancelHolder()
	select {
	case <-lease.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("lease context was not cancelled with its holder")
	}

	// Canceling the holder stops renewal without calling Close, modeling a
	// process that disappeared and left its lease to expire.
	time.Sleep(s.leaseTTL + 500*time.Millisecond)

	newLease, err := s.AcquireLease(context.Background(), "test-lease", "taker")
	if err != nil {
		t.Fatalf("AcquireLease after lease expiration failed: %v", err)
	}
	newLease.Close()
}

// TestAcquireLease_ConcurrentTakeover races many goroutines to acquire an
// already-expired lease against the real database, and asserts exactly one
// wins -- the property the doc's conditional-upsert SQL is meant to
// guarantee under real concurrency, which a single-connection unit test
// can't exercise.
func TestAcquireLease_ConcurrentTakeover(t *testing.T) {
	s := setupPostgresPersistence(t)
	s.leaseTTL = time.Millisecond
	holderCtx, cancelHolder := context.WithCancel(context.Background())
	initial, err := s.AcquireLease(holderCtx, "contested-lease", "holder")
	if err != nil {
		t.Fatalf("seeding initial lease failed: %v", err)
	}
	cancelHolder()
	<-initial.Context().Done()
	time.Sleep(50 * time.Millisecond) // let the 1ms lease expire.
	s.leaseTTL = 10 * time.Second

	const numRacers = 20
	winners := make(chan *store.Lease, numRacers)
	var wg sync.WaitGroup
	for i := 0; i < numRacers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			lease, err := s.AcquireLease(context.Background(), "contested-lease", "racer")
			if err != nil {
				if !errors.Is(err, store.ErrLeaseConflict) {
					t.Errorf("AcquireLease racer %d failed: %v", i, err)
				}
				return
			}
			// Keep the winning lease held until every racer has attempted
			// acquisition. Releasing it here would let later racers win
			// sequentially rather than testing concurrent takeover.
			winners <- lease
		}(i)
	}
	wg.Wait()
	close(winners)

	if got := len(winners); got != 1 {
		t.Errorf("expected exactly 1 racer to win the expired lease, got %d", got)
	}
	for lease := range winners {
		lease.Close()
	}
}
