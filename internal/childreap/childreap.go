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

// Package childreap reaps orphaned children without racing subprocess waits.
// A subprocess run through RunCommand is tracked by its PID and never reaped
// here, so its exit status stays with os/exec, and orphans are collected while
// it runs. A subprocess run under Enter is not known to the reaper, so reaping
// waits for it to finish.
//go:build unix

package childreap

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// MaxDefer is how long reaping waits before blocking new Enter subprocesses.
const MaxDefer = 60 * time.Second

// MaxDrain is how long a forced drain waits before abandoning the reap.
const MaxDrain = 30 * time.Second

// Reaper collects orphaned children. The zero value is not usable; call New.
type Reaper struct {
	mu sync.Mutex
	// cond is broadcast when inFlight reaches zero or reaping ends.
	cond *sync.Cond
	// inFlight counts subprocesses run under Enter, whose PIDs the reaper
	// does not know.
	inFlight int
	// tracked counts the RunCommand subprocesses by PID; a reap skips them.
	tracked map[int]int
	// reaping blocks new subprocesses while wait4 runs.
	reaping bool
	// draining blocks new subprocesses until inFlight reaches zero.
	draining bool
	// abandoned records that a reap gave up with children left unreaped.
	abandoned bool
	// retry re-runs an abandoned reap once the subprocesses that held it off
	// are gone. SIGCHLD fires on a transition, so a child that died before the
	// abandoned round will never announce itself again.
	retry chan struct{}
}

func New() *Reaper {
	r := &Reaper{retry: make(chan struct{}, 1), tracked: map[int]int{}}
	r.cond = sync.NewCond(&r.mu)
	return r
}

// Enter prevents reaping until the returned function is called. Use it for a
// subprocess started outside RunCommand, whose PID the reaper cannot skip.
//
//	defer reaper.Enter()()
//
// Do not use Enter for long-lived subprocesses; they prevent reaping.
func (r *Reaper) Enter() func() {
	r.enter()
	return r.leave
}

// RunCommand runs cmd and keeps its exit status from the reaper. Orphans are
// still reaped while it runs, so a command that waits for an orphan to go
// away — runsc deleting a sandbox the pod's init inherited — sees it go.
func (r *Reaper) RunCommand(cmd *exec.Cmd) error {
	if err := r.start(cmd); err != nil {
		return err
	}
	defer r.untrack(cmd.Process.Pid)
	return cmd.Wait()
}

// CombinedOutput is RunCommand for callers that need cmd.CombinedOutput.
func (r *Reaper) CombinedOutput(cmd *exec.Cmd) ([]byte, error) {
	if cmd.Stdout != nil {
		return nil, errors.New("exec: Stdout already set")
	}
	if cmd.Stderr != nil {
		return nil, errors.New("exec: Stderr already set")
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := r.RunCommand(cmd)
	return out.Bytes(), err
}

// start starts cmd under the lock a reap holds while it collects, so no reap
// sees the new process before it is tracked.
func (r *Reaper) start(cmd *exec.Cmd) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := cmd.Start(); err != nil {
		return err
	}
	r.tracked[cmd.Process.Pid]++
	return nil
}

func (r *Reaper) untrack(pid int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tracked[pid]--; r.tracked[pid] <= 0 {
		delete(r.tracked, pid)
	}
}

func (r *Reaper) enter() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for r.reaping || r.draining {
		r.cond.Wait()
	}
	r.inFlight++
}

func (r *Reaper) leave() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inFlight--
	if r.inFlight < 0 {
		// The exclusion is now meaningless: a later reap would see a quiet
		// process that is not quiet and take a subprocess's exit status.
		panic("childreap: leave called more times than Enter")
	}
	if r.inFlight == 0 {
		if r.abandoned {
			r.abandoned = false
			select {
			case r.retry <- struct{}{}:
			default:
			}
		}
		r.cond.Broadcast()
	}
}

// Run reaps until ctx ends. Call it once in its own goroutine.
func (r *Reaper) Run(ctx context.Context) {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, unix.SIGCHLD)
	defer signal.Stop(sigs)

	for {
		select {
		case <-ctx.Done():
			return
		case <-sigs:
		case <-r.retry:
		}
		// A burst of SIGCHLD needs only one pass over wait4.
		drain(sigs)
		r.reapOnce(ctx)
	}
}

// drain empties ch without blocking.
func drain(ch <-chan os.Signal) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

// reapOnce waits until no subprocess runs under Enter, then collects every
// exited child except the tracked RunCommand subprocesses.
func (r *Reaper) reapOnce(ctx context.Context) {
	if !r.acquire(ctx) {
		return
	}
	defer r.release()
	// Listed without the lock: a child forked by RunCommand is tracked before
	// start releases the lock, so it is tracked by the time it is checked below.
	children, err := childPIDs()
	if err != nil {
		slog.WarnContext(ctx, "Listing children to reap failed", slog.Any("err", err))
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, pid := range children {
		if r.tracked[pid] > 0 {
			continue
		}
		if err := reap(pid); err != nil {
			slog.WarnContext(ctx, "Reaping a child failed", slog.Int("pid", pid), slog.Any("err", err))
		}
	}
}

// reap collects pid if it has exited.
func reap(pid int) error {
	for {
		var status unix.WaitStatus
		_, err := unix.Wait4(pid, &status, unix.WNOHANG, nil)
		switch err {
		case nil, unix.ECHILD:
			return nil
		case unix.EINTR:
			continue
		default:
			return err
		}
	}
}

// childPIDs lists this process's children, running or exited.
func childPIDs() ([]int, error) {
	if hasChildrenFiles() {
		return childPIDsFromTasks()
	}
	return childPIDsFromScan()
}

// hasChildrenFiles reports whether the kernel lists each task's children in
// /proc/<pid>/task/<tid>/children (CONFIG_PROC_CHILDREN).
var hasChildrenFiles = sync.OnceValue(func() bool {
	_, err := os.Stat("/proc/thread-self/children")
	return err == nil
})

// childPIDsFromTasks reads the children of every thread: a child belongs to
// the thread that forked it, and an orphan to the thread it was reparented to.
func childPIDsFromTasks() ([]int, error) {
	tasks, err := os.ReadDir("/proc/self/task")
	if err != nil {
		return nil, err
	}
	var children []int
	for _, task := range tasks {
		list, err := os.ReadFile("/proc/self/task/" + task.Name() + "/children")
		if err != nil {
			// The thread is gone already.
			continue
		}
		for _, field := range strings.Fields(string(list)) {
			if pid, err := strconv.Atoi(field); err == nil {
				children = append(children, pid)
			}
		}
	}
	return children, nil
}

// childPIDsFromScan finds the children among all processes' /proc/<pid>/stat.
func childPIDsFromScan() ([]int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	self := os.Getpid()
	var children []int
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		stat, err := os.ReadFile("/proc/" + entry.Name() + "/stat")
		if err != nil {
			// The process is gone already.
			continue
		}
		if ppid, ok := parentPID(stat); ok && ppid == self {
			children = append(children, pid)
		}
	}
	return children, nil
}

// parentPID reads the parent PID from the contents of /proc/<pid>/stat. The
// command name before it is parenthesized and may itself hold ") ", so the
// fields are counted from the last ')'.
func parentPID(stat []byte) (int, bool) {
	end := bytes.LastIndexByte(stat, ')')
	if end < 0 {
		return 0, false
	}
	fields := strings.Fields(string(stat[end+1:]))
	if len(fields) < 2 {
		return 0, false
	}
	ppid, err := strconv.Atoi(fields[1])
	return ppid, err == nil
}

// acquire waits for Enter subprocesses, blocking new ones after MaxDefer.
// It returns false if ctx ends or the drain exceeds MaxDrain.
func (r *Reaper) acquire(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	// Nothing to wait for is the ordinary case, and it needs no deadlines.
	if r.tryAcquire() {
		return true
	}
	return r.awaitAcquire(ctx)
}

// tryAcquire takes the reap if no subprocess runs under Enter, and reports whether
// it did.
func (r *Reaper) tryAcquire() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.inFlight > 0 || r.reaping {
		return false
	}
	r.draining = false
	r.reaping = true
	return true
}

// awaitAcquire is acquire once something is in the way, so it is the only path
// that pays for the deadlines.
func (r *Reaper) awaitAcquire(ctx context.Context) bool {
	// sync.Cond has no timed wait, so each deadline is a timer that broadcasts
	// and the loop reads the clock itself.
	startedAt := time.Now()
	drainAt := startedAt.Add(MaxDefer)
	giveUpAt := drainAt.Add(MaxDrain)
	for _, at := range []time.Time{drainAt, giveUpAt} {
		timer := time.AfterFunc(time.Until(at), r.wake)
		defer timer.Stop()
	}

	stop := context.AfterFunc(ctx, r.wake)
	defer stop()

	r.mu.Lock()
	defer r.mu.Unlock()
	for {
		if ctx.Err() != nil {
			r.stopDrainingLocked()
			return false
		}
		// Another reap is in progress (it drained on our behalf); let it.
		if r.reaping {
			r.cond.Wait()
			continue
		}
		now := time.Now()
		if r.inFlight == 0 {
			if !now.Before(drainAt) {
				slog.WarnContext(ctx, "Reaped children only after holding off subprocesses",
					slog.Duration("waited", now.Sub(startedAt)))
			}
			r.draining = false
			r.reaping = true
			return true
		}
		if !now.Before(giveUpAt) {
			// Reaping now could consume the Enter subprocess's exit status.
			slog.ErrorContext(ctx, "Gave up reaping: a subprocess has held off the reaper too long",
				slog.Duration("waited", now.Sub(startedAt)), slog.Int("inFlight", r.inFlight))
			r.abandoned = true
			r.stopDrainingLocked()
			return false
		}
		if !now.Before(drainAt) {
			r.draining = true
		}
		r.cond.Wait()
	}
}

// wake only signals acquire; timer callbacks may run after acquire returns.
func (r *Reaper) wake() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cond.Broadcast()
}

// stopDrainingLocked ends an abandoned drain. Callers hold r.mu.
func (r *Reaper) stopDrainingLocked() {
	r.draining = false
	r.cond.Broadcast()
}

func (r *Reaper) release() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reaping = false
	r.cond.Broadcast()
}
