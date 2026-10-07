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

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agent-substrate/substrate/internal/imagecache"
	"github.com/agent-substrate/substrate/internal/nodepath"
	v1 "github.com/google/go-containerregistry/pkg/v1"
)

func TestImageCacheGCTarget(t *testing.T) {
	const gib = int64(1 << 30)
	tests := []struct {
		name                string
		capacity, available uint64
		cacheSize, maxBytes int64
		highPct, lowPct     int
		want                int64
	}{
		{
			name:     "below high watermark: no target",
			capacity: 100 * uint64(gib), available: 30 * uint64(gib), // 70% used
			highPct: 85, lowPct: 80,
			want: 0,
		},
		{
			name:     "at high watermark: free down to low",
			capacity: 100 * uint64(gib), available: 10 * uint64(gib), // 90% used
			cacheSize: 50 * gib, // cache is big enough to cover the shortfall
			highPct:   85, lowPct: 80,
			// available must climb to 20% of capacity: free 20GiB - 10GiB.
			want: 10 * gib,
		},
		{
			name:     "exactly high watermark triggers",
			capacity: 100 * uint64(gib), available: 15 * uint64(gib), // 85% used
			cacheSize: 50 * gib,
			highPct:   85, lowPct: 80,
			want: 5 * gib,
		},
		{
			// The kubelet formula assumes it owns the filesystem; we don't.
			// A near-full boot disk shared with containerd/kubelet/logs must
			// not ask an 11 MiB cache to free 18.9 GiB — uncapped, that
			// evicts the entire cache on every tick forever (0% hit rate)
			// without materially moving disk usage.
			name:     "watermark target capped at what the cache holds",
			capacity: 105 * uint64(gib), available: 2 * uint64(gib), // ~98% used
			cacheSize: 11 << 20,
			highPct:   85, lowPct: 80,
			want: 11 << 20,
		},
		{
			name:     "empty cache under volume pressure: nothing to free",
			capacity: 100 * uint64(gib), available: 1 * uint64(gib),
			cacheSize: 0,
			highPct:   85, lowPct: 80,
			want: 0,
		},
		{
			name:     "max-bytes cap independent of watermarks",
			capacity: 100 * uint64(gib), available: 90 * uint64(gib), // 10% used
			cacheSize: 8 * gib, maxBytes: 5 * gib,
			highPct: 85, lowPct: 80,
			want: 3 * gib,
		},
		{
			name:     "both: larger target wins",
			capacity: 100 * uint64(gib), available: 10 * uint64(gib), // watermark target 10GiB
			cacheSize: 60 * gib, maxBytes: 40 * gib, // cap target 20GiB
			highPct: 85, lowPct: 80,
			want: 20 * gib,
		},
		{
			name:     "max-bytes zero means no cap",
			capacity: 100 * uint64(gib), available: 90 * uint64(gib),
			cacheSize: 500 * gib, maxBytes: 0,
			highPct: 85, lowPct: 80,
			want: 0,
		},
		{
			name:     "zero capacity: watermark half disabled",
			capacity: 0, available: 0,
			cacheSize: 2 * gib, maxBytes: gib,
			highPct: 85, lowPct: 80,
			want: gib,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := imageCacheGCTarget(tc.capacity, tc.available, tc.cacheSize, tc.maxBytes, tc.highPct, tc.lowPct)
			if got != tc.want {
				t.Errorf("imageCacheGCTarget() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestValidateImageCacheGCFlags(t *testing.T) {
	setFlags := func(period time.Duration, high, low int, minAge time.Duration) {
		*imageCacheGCPeriod = period
		*imageCacheHighPct = high
		*imageCacheLowPct = low
		*imageCacheMinAge = minAge
	}
	t.Cleanup(func() { setFlags(5*time.Minute, 85, 80, 2*time.Minute) })

	cases := []struct {
		name      string
		period    time.Duration
		high, low int
		minAge    time.Duration
		wantErr   bool
	}{
		{"defaults", 5 * time.Minute, 85, 80, 2 * time.Minute, false},
		{"boundary high=100 low=0", 5 * time.Minute, 100, 0, 0, false},
		{"zero period disables the periodic pass", 0, 85, 80, 0, false},
		{"negative period would silently disable the loop", -5 * time.Minute, 85, 80, 0, true},
		{"high over 100", 5 * time.Minute, 101, 80, 0, true},
		{"low equals high", 5 * time.Minute, 85, 85, 0, true},
		{"low above high", 5 * time.Minute, 85, 90, 0, true},
		{"negative low", 5 * time.Minute, 85, -1, 0, true},
		{"negative min-age inverts the veto", 5 * time.Minute, 85, 80, -time.Second, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setFlags(tc.period, tc.high, tc.low, tc.minAge)
			err := validateImageCacheGCFlags()
			if (err != nil) != tc.wantErr {
				t.Errorf("period=%v high=%d low=%d minAge=%v: err=%v, wantErr=%v", tc.period, tc.high, tc.low, tc.minAge, err, tc.wantErr)
			}
		})
	}
}

func TestImageCacheDirOutsideBasePath(t *testing.T) {
	cases := []struct {
		name string
		dir  string
		want bool
	}{
		{"inside", filepath.Join(nodepath.BasePath, "image-cache"), false},
		{"inside with doubled separator", nodepath.BasePath + "//image-cache", false},
		{"inside via dot-dot", nodepath.BasePath + "/x/../image-cache", false},
		{"base path itself is not inside", nodepath.BasePath, true},
		{"sibling with the base path as name prefix", nodepath.BasePath + "-other/image-cache", true},
		{"outside", "/var/lib/elsewhere/image-cache", true},
		{"dot-dot escaping the base path", nodepath.BasePath + "/../elsewhere/image-cache", true},
		{"relative resolves against the cwd, not the base path", "image-cache", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := imageCacheDirOutsideBasePath(tc.dir); got != tc.want {
				t.Errorf("imageCacheDirOutsideBasePath(%q) = %v, want %v", tc.dir, got, tc.want)
			}
		})
	}
}

type fakeGCStore struct {
	size         int64
	sizeErr      error
	sizeCalls    int
	evictCalls   int
	gotTarget    int64
	gotDryRun    bool
	evictErr     error
	stats        imagecache.EvictStats
	panicOnEvict bool
	ensured      []string
	ensureErr    error
	pinned       []string
	unpinned     []string
	// digests overrides the digest a reference resolves to; any other
	// reference resolves to sha256:aaa….
	digests map[string]string
}

func (f *fakeGCStore) EnsureImage(_ context.Context, ref string) (*imagecache.Image, error) {
	f.ensured = append(f.ensured, ref)
	if f.ensureErr != nil {
		return nil, f.ensureErr
	}
	hex := strings.Repeat("a", 64)
	if d, ok := f.digests[ref]; ok {
		hex = d
	}
	return &imagecache.Image{Digest: v1.Hash{Algorithm: "sha256", Hex: hex}}, nil
}

func (f *fakeGCStore) Pin(img *imagecache.Image) {
	f.pinned = append(f.pinned, img.Digest.String())
}

func (f *fakeGCStore) Unpin(digest string) {
	f.unpinned = append(f.unpinned, digest)
}

func (f *fakeGCStore) CacheSize() (int64, error) {
	f.sizeCalls++
	return f.size, f.sizeErr
}

func (f *fakeGCStore) EvictUnused(_ context.Context, target int64, dryRun bool) (imagecache.EvictStats, error) {
	f.evictCalls++
	f.gotTarget = target
	f.gotDryRun = dryRun
	if f.panicOnEvict {
		panic("boom")
	}
	return f.stats, f.evictErr
}

func TestRunPassSkipsOnStatfsFailure(t *testing.T) {
	fake := &fakeGCStore{}
	g := &imageCacheGC{store: fake, cacheDir: filepath.Join(t.TempDir(), "missing"), highPct: 85, lowPct: 80}
	g.runPass(context.Background())
	if fake.sizeCalls != 0 || fake.evictCalls != 0 {
		t.Errorf("statfs failure: sizeCalls=%d evictCalls=%d, want 0/0", fake.sizeCalls, fake.evictCalls)
	}
}

func TestRunPassSkipsOnCacheSizeFailure(t *testing.T) {
	fake := &fakeGCStore{sizeErr: errors.New("unreadable size file")}
	g := &imageCacheGC{store: fake, cacheDir: t.TempDir(), highPct: 85, lowPct: 80}
	g.runPass(context.Background())
	if fake.evictCalls != 0 {
		t.Errorf("CacheSize failure: evictCalls=%d, want 0", fake.evictCalls)
	}
}

func TestRunPassEvictsAndPassesDryRun(t *testing.T) {
	// high=100 sidelines the watermark on any volume with >=1% free, so
	// the max-bytes overage (99) is the target; a near-full host volume
	// can lift it to the cacheSize cap, hence >= not ==.
	fake := &fakeGCStore{size: 100, stats: imagecache.EvictStats{FreedBytes: 100}}
	g := &imageCacheGC{store: fake, cacheDir: t.TempDir(), highPct: 100, lowPct: 0, maxBytes: 1, dryRun: true}
	g.consecutiveShortfalls = 5 // a met target must reset it
	g.runPass(context.Background())
	if fake.evictCalls != 1 || fake.gotTarget < 99 || !fake.gotDryRun {
		t.Errorf("evictCalls=%d target=%d dryRun=%v, want 1/>=99/true", fake.evictCalls, fake.gotTarget, fake.gotDryRun)
	}
	if g.consecutiveShortfalls != 0 {
		t.Errorf("consecutiveShortfalls=%d after met target, want 0", g.consecutiveShortfalls)
	}
}

func TestRunFirstPassIsImmediate(t *testing.T) {
	// A cancelled context and an hour-long period: the single call can
	// only be the immediate first pass, never a tick.
	fake := &fakeGCStore{}
	g := &imageCacheGC{store: fake, cacheDir: t.TempDir(), highPct: 100, lowPct: 0, period: time.Hour}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	g.Run(ctx)
	if fake.evictCalls != 1 {
		t.Errorf("evictCalls=%d, want exactly 1 (the immediate first pass)", fake.evictCalls)
	}
}

func TestRunTicks(t *testing.T) {
	fake := &fakeGCStore{}
	g := &imageCacheGC{store: fake, cacheDir: t.TempDir(), highPct: 100, lowPct: 0, period: 10 * time.Millisecond}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	g.Run(ctx)
	if fake.evictCalls < 2 {
		t.Errorf("evictCalls=%d, want >=2 (first pass plus at least one tick)", fake.evictCalls)
	}
}

func TestRunPassEnsuresAndPinsBeforeEvicting(t *testing.T) {
	fake := &fakeGCStore{size: 100, stats: imagecache.EvictStats{FreedBytes: 100}}
	g := &imageCacheGC{store: fake, cacheDir: t.TempDir(), highPct: 100, lowPct: 0, maxBytes: 1,
		pinned: []string{"registry.example/app-a:1", "registry.example/app-b:2"}}
	g.runPass(context.Background())
	if len(fake.ensured) != 2 || len(fake.pinned) != 2 {
		t.Errorf("ensured=%v pinned=%v, want both pins pulled and pinned", fake.ensured, fake.pinned)
	}
	if fake.evictCalls != 1 {
		t.Errorf("evictCalls=%d, want 1", fake.evictCalls)
	}
}

func TestRunPassRunsWhenAPinnedPullFails(t *testing.T) {
	fake := &fakeGCStore{size: 100, ensureErr: errors.New("registry down")}
	g := &imageCacheGC{store: fake, cacheDir: t.TempDir(), highPct: 100, lowPct: 0, maxBytes: 1,
		pinned: []string{"registry.example/app-a:1"}}
	g.runPass(context.Background())
	if len(fake.pinned) != 0 {
		t.Errorf("pinned=%v after a failed pull, want none", fake.pinned)
	}
	if fake.evictCalls != 1 {
		t.Errorf("evictCalls=%d, want 1: a failed pin must not gate the pass", fake.evictCalls)
	}
}

func hexOf(c string) string { return strings.Repeat(c, 64) }

func writePinnedFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEnsurePinnedJoinsFlagAndFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "pinned-images")
	writePinnedFile(t, file, "# kagent images\n\nregistry.example/app-b:2\n  registry.example/app-a:1  \nnot a reference\n")
	fake := &fakeGCStore{}
	g := &imageCacheGC{store: fake, pinned: []string{"registry.example/app-a:1"}, pinnedFile: file}
	g.ensurePinned(context.Background())
	want := []string{"registry.example/app-a:1", "registry.example/app-b:2"}
	if fmt.Sprint(fake.ensured) != fmt.Sprint(want) {
		t.Errorf("ensured=%v, want %v (flag first, file once each, comments and malformed lines skipped)", fake.ensured, want)
	}
}

func TestEnsurePinnedUnpinsWhatLeftTheFile(t *testing.T) {
	file := filepath.Join(t.TempDir(), "pinned-images")
	fake := &fakeGCStore{digests: map[string]string{
		"registry.example/old:1":  hexOf("1"),
		"registry.example/new:2":  hexOf("2"),
		"registry.example/keep:1": hexOf("3"),
	}}
	g := &imageCacheGC{store: fake, pinnedFile: file}

	writePinnedFile(t, file, "registry.example/old:1\nregistry.example/keep:1\n")
	g.ensurePinned(context.Background())
	if len(fake.unpinned) != 0 {
		t.Fatalf("unpinned=%v on the first pass, want none", fake.unpinned)
	}

	writePinnedFile(t, file, "registry.example/new:2\nregistry.example/keep:1\n")
	g.ensurePinned(context.Background())
	if want := []string{"sha256:" + hexOf("1")}; fmt.Sprint(fake.unpinned) != fmt.Sprint(want) {
		t.Errorf("unpinned=%v, want only the image that left the list %v", fake.unpinned, want)
	}
}

func TestEnsurePinnedUnpinsTheDigestATagMovedFrom(t *testing.T) {
	fake := &fakeGCStore{digests: map[string]string{"registry.example/app-a:latest": hexOf("1")}}
	g := &imageCacheGC{store: fake, pinned: []string{"registry.example/app-a:latest"}}
	g.ensurePinned(context.Background())
	fake.digests["registry.example/app-a:latest"] = hexOf("2")
	g.ensurePinned(context.Background())
	if want := []string{"sha256:" + hexOf("1")}; fmt.Sprint(fake.unpinned) != fmt.Sprint(want) {
		t.Errorf("unpinned=%v, want the digest the tag moved from %v", fake.unpinned, want)
	}
}

func TestEnsurePinnedKeepsAPinWhosePullFails(t *testing.T) {
	fake := &fakeGCStore{}
	g := &imageCacheGC{store: fake, pinned: []string{"registry.example/app-a:1"}}
	g.ensurePinned(context.Background())
	fake.ensureErr = errors.New("registry down")
	g.ensurePinned(context.Background())
	if len(fake.unpinned) != 0 {
		t.Errorf("unpinned=%v after a failed re-pull, want the earlier pin kept", fake.unpinned)
	}
}

func TestEnsurePinnedKeepsPinsWhenTheFileIsUnreadable(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "pinned-images")
	writePinnedFile(t, file, "registry.example/app-a:1\n")
	fake := &fakeGCStore{}
	g := &imageCacheGC{store: fake, pinnedFile: file}
	g.ensurePinned(context.Background())

	// A directory in the file's place: reading it fails with something
	// other than not-exist.
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(file, 0o755); err != nil {
		t.Fatal(err)
	}
	g.ensurePinned(context.Background())
	if len(fake.unpinned) != 0 {
		t.Errorf("unpinned=%v with an unreadable file, want the pins kept", fake.unpinned)
	}
}

func TestEnsurePinnedMissingFileIsAnEmptyList(t *testing.T) {
	file := filepath.Join(t.TempDir(), "pinned-images")
	writePinnedFile(t, file, "registry.example/app-a:1\n")
	fake := &fakeGCStore{}
	g := &imageCacheGC{store: fake, pinnedFile: file}
	g.ensurePinned(context.Background())
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	g.ensurePinned(context.Background())
	if want := []string{"sha256:" + hexOf("a")}; fmt.Sprint(fake.unpinned) != fmt.Sprint(want) {
		t.Errorf("unpinned=%v once the file is gone, want %v", fake.unpinned, want)
	}
}

func TestRunPinsOnlyRefreshes(t *testing.T) {
	fake := &fakeGCStore{}
	g := &imageCacheGC{store: fake, pinned: []string{"registry.example/app-a:1"}}
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	g.RunPinsOnly(ctx, 10*time.Millisecond)
	if len(fake.ensured) < 2 {
		t.Errorf("ensured=%v, want the immediate refresh plus at least one tick", fake.ensured)
	}
	if fake.evictCalls != 0 {
		t.Errorf("evictCalls=%d, want 0: the pins-only loop never evicts", fake.evictCalls)
	}
}

func TestValidateImageCacheGCFlagsRejectsMalformedPin(t *testing.T) {
	saved := *imageCachePinned
	t.Cleanup(func() { *imageCachePinned = saved })
	*imageCachePinned = []string{"registry.example/ok:1", "not a reference"}
	if err := validateImageCacheGCFlags(); err == nil {
		t.Error("malformed pinned image reference accepted")
	}
	*imageCachePinned = []string{"registry.example/ok:1", "registry.example/ok@sha256:" + strings.Repeat("0", 64)}
	if err := validateImageCacheGCFlags(); err != nil {
		t.Errorf("valid pinned image references rejected: %v", err)
	}
}

func TestRunPassRecoversPanic(t *testing.T) {
	g := &imageCacheGC{store: &fakeGCStore{panicOnEvict: true}, cacheDir: t.TempDir(), highPct: 100, lowPct: 0}
	g.runPass(context.Background()) // must not propagate the panic
}

// TestNoteOutcomeShortfallBackoff drives the shortfall cadence end to end:
// warn on the first shortfallWarnLimit consecutive shortfalls, then only
// every shortfallReminderEvery-th, streak preserved across a gated pass,
// reset (re-arming the warnings) on a met or absent target.
func TestNoteOutcomeShortfallBackoff(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	ctx := context.Background()
	g := &imageCacheGC{}
	logCount := func(msg string) int { return strings.Count(buf.String(), msg) }

	for range shortfallWarnLimit {
		g.noteOutcome(ctx, gcPassShortfall, nil, nil)
	}
	if got := logCount("could not reach target"); got != shortfallWarnLimit {
		t.Errorf("initial warns = %d, want %d", got, shortfallWarnLimit)
	}

	buf.Reset()
	for g.consecutiveShortfalls < 2*shortfallReminderEvery {
		g.noteOutcome(ctx, gcPassShortfall, nil, nil)
	}
	if got := logCount("still short of target"); got != 2 {
		t.Errorf("reminders through streak %d = %d, want 2", g.consecutiveShortfalls, got)
	}
	if got := logCount("could not reach target"); got != 0 {
		t.Errorf("warns past the limit = %d, want 0", got)
	}

	streak := g.consecutiveShortfalls
	buf.Reset()
	g.noteOutcome(ctx, gcPassSkipped, errors.New("gated"), nil)
	if g.consecutiveShortfalls != streak {
		t.Errorf("streak after gated pass = %d, want %d (preserved)", g.consecutiveShortfalls, streak)
	}
	if got := logCount("Image cache GC pass skipped"); got != 1 {
		t.Errorf("skip logs = %d, want 1", got)
	}

	buf.Reset()
	g.noteOutcome(ctx, gcPassComplete, errors.New("one dir failed"), nil)
	if g.consecutiveShortfalls != 0 {
		t.Errorf("streak after complete pass = %d, want 0", g.consecutiveShortfalls)
	}
	if got := logCount("pass complete"); got != 1 {
		t.Errorf("complete logs = %d, want 1", got)
	}
	if got := logCount("finished with errors"); got != 1 {
		t.Errorf("per-item error warns = %d, want 1", got)
	}

	buf.Reset()
	g.noteOutcome(ctx, gcPassShortfall, nil, nil)
	if got := logCount("could not reach target"); got != 1 {
		t.Errorf("warns after reset = %d, want 1 (re-armed)", got)
	}

	g.consecutiveShortfalls = shortfallWarnLimit + 1
	buf.Reset()
	g.noteOutcome(ctx, gcPassQuiet, nil, nil)
	if g.consecutiveShortfalls != 0 || buf.Len() != 0 {
		t.Errorf("quiet pass: streak=%d buf=%q, want silent reset", g.consecutiveShortfalls, buf.String())
	}
}

func TestClassifyGCPass(t *testing.T) {
	gated := fmt.Errorf("pass gated: %w", imagecache.ErrIncompleteEnumeration)
	perItem := errors.New("while removing retired layer: permission denied")
	cases := []struct {
		name          string
		err           error
		target, freed int64
		want          gcPassOutcome
	}{
		{"gated pass", gated, 100, 0, gcPassSkipped},
		{"gated wins even with zero target", gated, 0, 0, gcPassSkipped},
		{"per-item errors are not a skip", perItem, 100, 100, gcPassComplete},
		{"per-item errors with shortfall", perItem, 100, 40, gcPassShortfall},
		{"shortfall", nil, 100, 40, gcPassShortfall},
		{"target met", nil, 100, 100, gcPassComplete},
		{"no target", nil, 0, 0, gcPassQuiet},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyGCPass(tc.err, tc.target, tc.freed); got != tc.want {
				t.Errorf("classifyGCPass(%v, %d, %d) = %d, want %d", tc.err, tc.target, tc.freed, got, tc.want)
			}
		})
	}
}
