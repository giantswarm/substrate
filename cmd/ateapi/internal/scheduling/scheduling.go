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

// Package scheduling decides which worker should host an actor.
package scheduling

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"slices"
	"strings"

	"github.com/agent-substrate/substrate/internal/resources"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"k8s.io/apimachinery/pkg/labels"
)

// Constraints describes what a worker must satisfy to host an actor.
type Constraints struct {
	// SandboxClass must equal the worker's sandbox class. Snapshots are not
	// portable across sandbox classes, so this is never relaxed.
	SandboxClass string

	// TemplateSelector and ActorSelector must both match the worker's labels.
	TemplateSelector labels.Selector
	ActorSelector    labels.Selector

	// RequiredNodes, when non-empty, restricts placement to workers running
	// on one of these nodes. Used when the actor's latest snapshot is local
	// to specific node VMs and exists nowhere else.
	RequiredNodes []string

	// PreferredNodes, when non-empty, ranks workers running on one of these
	// nodes ahead of every other eligible worker without excluding any. Used
	// when the actor's latest snapshot is local to these nodes but also has a
	// durable copy: the node is the fast path, not the only one.
	PreferredNodes []string

	// Limits are the actor's declared resource limits, named as a Worker names
	// the capacity it reports, so the two subtract.
	Limits *ateapipb.Resources
}

// ErrNoCapacity is returned by Schedule when no worker that meets the
// constraints has room. The cause can be full workers, or no worker that meets
// the constraints at all.
var ErrNoCapacity = errors.New("no worker that meets the constraints has room")

// NoCapacityError is the ErrNoCapacity of a placement restricted to nodes (a
// PAUSED actor whose local snapshot lives on them). It carries what the one
// pass over the fleet saw on those nodes, so the caller can tell a snapshot
// locality conflict from a full pool -- and a node that no longer hosts any
// worker, which may mean the snapshot is gone with it. It matches
// ErrNoCapacity through errors.Is.
type NoCapacityError struct {
	// RequiredNodes is the restriction that was in effect.
	RequiredNodes []string
	// EligibleOnRequiredNodes counts the workers on those nodes that satisfy
	// every other constraint; all of them were full.
	EligibleOnRequiredNodes int
	// WorkersOnRequiredNodes counts the ACTIVE workers of any pool on those
	// nodes. Zero means no worker reports any of the nodes at all.
	WorkersOnRequiredNodes int
}

func (e *NoCapacityError) Error() string {
	switch {
	case e.EligibleOnRequiredNodes > 0:
		return fmt.Sprintf("%v: the %d eligible worker(s) on node(s) %v are full", ErrNoCapacity, e.EligibleOnRequiredNodes, e.RequiredNodes)
	case e.WorkersOnRequiredNodes > 0:
		return fmt.Sprintf("%v: no eligible worker on node(s) %v (%d worker(s) of other pools report them)", ErrNoCapacity, e.RequiredNodes, e.WorkersOnRequiredNodes)
	default:
		return fmt.Sprintf("%v: no worker reports node(s) %v", ErrNoCapacity, e.RequiredNodes)
	}
}

// Is makes errors.Is(err, ErrNoCapacity) hold for a NoCapacityError.
func (e *NoCapacityError) Is(target error) bool { return target == ErrNoCapacity }

// NoMatchingWorkerError is the ErrNoCapacity of a placement whose worker
// selectors match the labels of none of the fleet's workers, whatever their
// state, sandbox class or room. It tells a label mismatch (a selector or a
// pool's labels renamed on one side only) from a full pool, which waiting does
// not cure. It matches ErrNoCapacity through errors.Is.
type NoMatchingWorkerError struct {
	// Selector is the template's and the actor's worker selectors as one.
	Selector string
	// Workers counts the fleet's workers, none of which the selector matches.
	Workers int
}

func (e *NoMatchingWorkerError) Error() string {
	return fmt.Sprintf("%v: the selector %q matches none of the %d worker(s)", ErrNoCapacity, e.Selector, e.Workers)
}

// Is makes errors.Is(err, ErrNoCapacity) hold for a NoMatchingWorkerError.
func (e *NoMatchingWorkerError) Is(target error) bool { return target == ErrNoCapacity }

// Scheduler answers placement questions against the current worker fleet.
type Scheduler interface {
	// Schedule returns a worker that meets constraints and has room.
	// Returns ErrNoCapacity when no worker that meets constraints has room.
	Schedule(ctx context.Context, constraints Constraints) (*ateapipb.Worker, error)

	// Applies reports whether worker satisfies non-capacity constraints. Capacity
	// is excluded so an existing assignment does not make its Worker ineligible.
	Applies(worker *ateapipb.Worker, constraints Constraints) bool

	// HasRoom reports whether worker's remaining capacity admits one more actor
	// of this size, in every dimension. Independent of Applies.
	HasRoom(worker *ateapipb.Worker, constraints Constraints) bool
}

// WorkerSource provides the whole fleet of workers.
type WorkerSource interface {
	Workers() ([]*ateapipb.Worker, error)
}

type scheduler struct {
	source WorkerSource
	// intn returns a uniformly distributed random value in [0,n).
	// Defaults to the global math/rand source
	intn func(n int) int
}

// Option configures the Scheduler returned by New.
type Option func(*scheduler)

// WithIntn overrides the random source used to sample candidate pairs: when
// n >= 2 candidates exist, Schedule calls intn(n) and then intn(n-1).
func WithIntn(intn func(n int) int) Option {
	return func(s *scheduler) { s.intn = intn }
}

// New returns a Scheduler placing onto workers reported by source.
func New(source WorkerSource, opts ...Option) Scheduler {
	s := &scheduler{source: source, intn: rand.Intn}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Schedule filters the fleet for eligible candidates with room, samples two at
// random (power of two choices), and returns the less-loaded one, where load is
// the higher of actor-slot and compute-resource utilization. Spreading across
// the warm pool avoids hotspots until autoscaling reclaims idle workers.
func (s *scheduler) Schedule(ctx context.Context, constraints Constraints) (*ateapipb.Worker, error) {
	workers, err := s.source.Workers()
	if err != nil {
		return nil, fmt.Errorf("while listing workers: %w", err)
	}

	want, err := resources.ParseQuantities(constraints.Limits)
	if err != nil {
		return nil, fmt.Errorf("while parsing actor resource limits: %w", err)
	}

	var candidates []candidate
	// eligible counts the workers Applies admits, with or without room.
	eligible := 0
	// Under a node restriction, every ACTIVE worker on one of the nodes is
	// counted whatever its pool: the count tells a node that still hosts
	// workers from one that hosts none.
	onRequiredNodes := 0
	// selected counts the workers whose labels the selectors match, in any
	// state and of any sandbox class.
	selected := 0
	for _, worker := range workers {
		if len(constraints.RequiredNodes) > 0 &&
			worker.GetStatus().GetState() == ateapipb.WorkerState_WORKER_STATE_ACTIVE &&
			slices.Contains(constraints.RequiredNodes, worker.GetNodeName()) {
			onRequiredNodes++
		}
		if matchesSelectors(worker, constraints) {
			selected++
		}
		if !s.Applies(worker, constraints) {
			continue
		}
		eligible++
		if cand, ok := checkRoom(worker, want); ok {
			candidates = append(candidates, cand)
		}
	}

	if len(candidates) == 0 {
		if len(constraints.RequiredNodes) > 0 {
			return nil, &NoCapacityError{
				RequiredNodes:           slices.Clone(constraints.RequiredNodes),
				EligibleOnRequiredNodes: eligible,
				WorkersOnRequiredNodes:  onRequiredNodes,
			}
		}
		// An empty fleet is a pool still scaling up, not a label mismatch.
		if selector := selectorString(constraints); selected == 0 && len(workers) > 0 && selector != "" {
			return nil, &NoMatchingWorkerError{Selector: selector, Workers: len(workers)}
		}
		return nil, ErrNoCapacity
	}
	if preferred := candidatesOnNodes(candidates, constraints.PreferredNodes); len(preferred) > 0 {
		candidates = preferred
	}
	if len(candidates) == 1 {
		return candidates[0].worker, nil
	}

	i := s.intn(len(candidates))
	j := s.intn(len(candidates) - 1)
	if j >= i {
		j++
	}
	if lessLoaded(&candidates[j], &candidates[i]) {
		return candidates[j].worker, nil
	}
	return candidates[i].worker, nil
}

// candidate pairs an eligible worker with its cached compute utilization.
type candidate struct {
	worker *ateapipb.Worker
	// resUtil is the worker's dominant compute-resource utilization in [0,1],
	// or -1 if not yet computed.
	resUtil float64
}

func (c *candidate) resourceUtilization() float64 {
	if c.resUtil < 0 {
		c.resUtil = workerResourceUtilization(c.worker)
	}
	return c.resUtil
}

// lessLoaded compares dominant utilization — the higher of actor-slot
// utilization (allocated/capacity) and compute-resource utilization — so either
// dimension can mark a worker as hot. Ties fall back to actor-slot utilization,
// then to remaining actor slots.
func lessLoaded(a, b *candidate) bool {
	aAlloc := int64(a.worker.GetStatus().GetAllocated().GetActors())
	bAlloc := int64(b.worker.GetStatus().GetAllocated().GetActors())
	// checkRoom admits only workers with allocated < capacity, so capacity >= 1.
	aCap := int64(a.worker.GetStatus().GetCapacity().GetActors())
	bCap := int64(b.worker.GetStatus().GetCapacity().GetActors())

	aLoad := max(float64(aAlloc)/float64(aCap), a.resourceUtilization())
	bLoad := max(float64(bAlloc)/float64(bCap), b.resourceUtilization())
	if aLoad != bLoad {
		return aLoad < bLoad
	}

	// Compare slot utilization exactly via cross-multiplication.
	if lhs, rhs := aAlloc*bCap, bAlloc*aCap; lhs != rhs {
		return lhs < rhs
	}

	return aCap-aAlloc > bCap-bAlloc
}

// workerResourceUtilization returns the highest allocated/capacity ratio across
// compute dimensions, or 1 when capacity is unreported or quantities fail to parse.
func workerResourceUtilization(w *ateapipb.Worker) float64 {
	capQ, err := resources.ParseQuantities(w.GetStatus().GetCapacity().GetResources())
	if err != nil || len(capQ) == 0 {
		return 1
	}
	usedQ, err := resources.ParseQuantities(w.GetStatus().GetAllocated().GetResources())
	if err != nil {
		return 1
	}
	return quantitiesUtilization(capQ, usedQ)
}

func quantitiesUtilization(capQ, usedQ resources.Quantities) float64 {
	var (
		maxRatio    float64
		hasPositive bool
	)
	for name, capVal := range capQ {
		if capFloat := capVal.AsApproximateFloat64(); capFloat > 0 {
			hasPositive = true
			if usedVal, ok := usedQ[name]; ok {
				maxRatio = max(maxRatio, usedVal.AsApproximateFloat64()/capFloat)
			}
		}
	}
	if !hasPositive {
		return 1
	}
	return maxRatio
}

// candidatesOnNodes returns the candidates running on one of nodes; none for an
// empty nodes.
func candidatesOnNodes(candidates []candidate, nodes []string) []candidate {
	if len(nodes) == 0 {
		return nil
	}
	var onNodes []candidate
	for _, cand := range candidates {
		if slices.Contains(nodes, cand.worker.GetNodeName()) {
			onNodes = append(onNodes, cand)
		}
	}
	return onNodes
}

func (s *scheduler) Applies(worker *ateapipb.Worker, constraints Constraints) bool {
	if worker.GetSandboxClass() != constraints.SandboxClass {
		return false
	}

	if worker.GetStatus().GetState() != ateapipb.WorkerState_WORKER_STATE_ACTIVE {
		return false
	}

	if !matchesSelectors(worker, constraints) {
		return false
	}

	return len(constraints.RequiredNodes) == 0 || slices.Contains(constraints.RequiredNodes, worker.GetNodeName())
}

// matchesSelectors reports whether both of the constraints' worker selectors
// match the worker's labels; a nil selector matches every worker.
func matchesSelectors(worker *ateapipb.Worker, constraints Constraints) bool {
	set := labels.Set(worker.GetLabels())
	if constraints.TemplateSelector != nil && !constraints.TemplateSelector.Matches(set) {
		return false
	}
	return constraints.ActorSelector == nil || constraints.ActorSelector.Matches(set)
}

// selectorString renders the template's and the actor's worker selectors as
// the one selector a worker must match, "" when neither restricts.
func selectorString(constraints Constraints) string {
	var parts []string
	for _, sel := range []labels.Selector{constraints.TemplateSelector, constraints.ActorSelector} {
		if sel != nil && !sel.Empty() {
			parts = append(parts, sel.String())
		}
	}
	return strings.Join(parts, ",")
}

// HasRoom reports whether what the worker has left admits one more actor of
// this size. A dimension the worker does not report is unconstrained, so
// placement is never blocked by missing data.
//
// A worker whose recorded capacity or allocation will not parse is treated as
// having no room: it is the only answer that cannot overcommit a worker whose
// true occupancy is unreadable.
func (s *scheduler) HasRoom(worker *ateapipb.Worker, constraints Constraints) bool {
	want, err := resources.ParseQuantities(constraints.Limits)
	if err != nil {
		return false
	}
	_, ok := checkRoom(worker, want)
	return ok
}

func checkRoom(worker *ateapipb.Worker, want resources.Quantities) (candidate, bool) {
	capacity := worker.GetStatus().GetCapacity()
	used := worker.GetStatus().GetAllocated()

	// No per-actor size to compare: every assignment costs one, so a worker at
	// its limit has no room however small the next actor is.
	if used.GetActors() >= capacity.GetActors() {
		return candidate{}, false
	}

	if len(want) == 0 {
		return candidate{worker: worker, resUtil: -1}, true
	}
	free, err := resources.ParseQuantities(capacity.GetResources())
	if err != nil {
		return candidate{}, false
	}
	if free == nil {
		free = resources.Quantities{}
	}
	allocated, err := resources.ParseQuantities(used.GetResources())
	if err != nil {
		return candidate{}, false
	}
	resUtil := quantitiesUtilization(free, allocated)
	free.Sub(allocated)
	if !free.Covers(want) {
		return candidate{}, false
	}
	return candidate{worker: worker, resUtil: resUtil}, true
}
