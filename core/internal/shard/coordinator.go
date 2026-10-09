package shard

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Options configures one Run.
type Options struct {
	Launcher Launcher
	// Devices are the devices to start a worker for.
	Devices []Device
	// Paths are the entry paths in suite order.
	Paths []string
	// Args builds the worker command line for a device.
	Args func(Device) []string
	// StopGrace is how long a worker gets to stop on its own after an
	// interrupt before it is ended.
	StopGrace time.Duration
	// Warn receives one line per notable event, such as a dropped worker.
	// Nil discards them.
	Warn func(string)
}

// Outcome is what Run produced.
type Outcome struct {
	// Entries holds every entry that ran, in suite order. An entry that
	// ran in two workers lists the attempts of both, each stamped with its
	// device and worker run.
	Entries []Entry
	// Unrun holds the paths no worker ran because every worker stopped,
	// in suite order.
	Unrun []string
	// Ready holds the devices whose worker reported ready, in the order of
	// Options.Devices. A device whose worker could not start or was dropped
	// before it was ready is not listed.
	Ready []Device
	// Interrupted is true when ctx ended the run.
	Interrupted bool
}

// requeueLimit is how many times an entry whose worker lost its device goes
// back to the queue for another worker.
const requeueLimit = 1

type job struct {
	index    int
	path     string
	kind     string
	attempts []Attempt
	requeues int
}

type slot struct {
	device     Device
	worker     Worker
	ready      bool
	closing    bool
	ended      bool
	stopped    bool
	lost       bool
	current    *job
	dropReason string
}

type event struct {
	slot *slot
	line *Line
}

// Run feeds paths to one worker per device and collects their reports.
// Each worker gets its next path when it reported the previous one, so a
// fast device takes more entries than a slow one. A worker that cannot
// start or reports an error before it is ready is dropped, and an entry
// whose worker lost its device goes back to the queue once. Run returns
// when every worker has ended.
func Run(ctx context.Context, opts Options) Outcome {
	c := &coordinator{opts: opts, results: make([]*Entry, len(opts.Paths))}
	for i, path := range opts.Paths {
		c.queue = append(c.queue, &job{index: i, path: path})
	}
	events := make(chan event)
	for _, device := range opts.Devices {
		worker, err := opts.Launcher.Launch(ctx, WorkerSpec{Device: device, Args: opts.Args(device)})
		if err != nil {
			c.warn("device %s dropped: its worker could not start: %v", device.ID, err)
			continue
		}
		s := &slot{device: device, worker: worker}
		c.slots = append(c.slots, s)
		go func() {
			for line := range worker.Lines() {
				events <- event{slot: s, line: &line}
			}
			events <- event{slot: s}
		}()
	}

	done := ctx.Done()
	live := len(c.slots)
	for live > 0 {
		select {
		case <-done:
			done = nil
			c.interrupted = true
			for _, s := range c.slots {
				if !s.ended {
					c.stop(s)
				}
			}
		case ev := <-events:
			if ev.line != nil {
				c.handle(ev.slot, *ev.line)
			} else {
				live--
				c.end(ev.slot)
			}
			c.dispatch()
		}
	}
	c.stopping.Wait()
	return c.outcome()
}

type coordinator struct {
	opts        Options
	slots       []*slot
	queue       []*job
	results     []*Entry
	interrupted bool
	stopping    sync.WaitGroup
}

// stop ends a worker in the background, first asking it to stop on its own.
func (c *coordinator) stop(s *slot) {
	if s.stopped {
		return
	}
	s.stopped = true
	c.stopping.Add(1)
	go func() {
		defer c.stopping.Done()
		s.worker.Stop(c.opts.StopGrace)
	}()
}

func (c *coordinator) warn(format string, args ...any) {
	if c.opts.Warn != nil {
		c.opts.Warn(fmt.Sprintf(format, args...))
	}
}

func (c *coordinator) handle(s *slot, line Line) {
	switch line.Kind {
	case LineReady:
		s.ready = true
	case LineError:
		if line.Code == DeviceLostCode {
			c.finishIdleLoss(s)
			return
		}
		s.dropReason = strings.TrimSpace(line.Code + " " + line.Message)
	case LineEntry:
		c.finishEntry(s, line)
	}
}

// finishIdleLoss settles a worker that lost its device while it ran no
// entry. An entry it was sent never started, so it goes back to the front
// of the queue without using up its requeue.
func (c *coordinator) finishIdleLoss(s *slot) {
	s.lost = true
	_ = s.worker.CloseInput()
	c.warn("device %s was lost, its worker stopped", s.device.ID)
	if j := s.current; j != nil {
		s.current = nil
		c.queue = append([]*job{j}, c.queue...)
	}
}

// finishEntry records the entry a worker reported. An entry that failed
// because its worker lost the device goes back to the front of the queue
// the first time, so another worker runs it.
func (c *coordinator) finishEntry(s *slot, line Line) {
	j := s.current
	if j == nil {
		return
	}
	if err := validateEntry(line.Entry); err != nil {
		c.rejectReport(s, err.Error())
		return
	}
	if line.Entry.Path != j.path {
		c.rejectReport(s, fmt.Sprintf("it reported %s while running %s", line.Entry.Path, j.path))
		return
	}
	s.current = nil
	attempts := make([]Attempt, len(line.Entry.Attempts))
	for i, attempt := range line.Entry.Attempts {
		attempt.DeviceID, attempt.WorkerRunID = s.device.ID, line.RunID
		attempts[i] = attempt
	}
	if line.DeviceLost {
		s.lost = true
		_ = s.worker.CloseInput()
		c.warn("device %s was lost, its worker stopped", s.device.ID)
		if line.Entry.Status != statusPassed && j.requeues < requeueLimit {
			j.requeues++
			j.kind = line.Entry.Kind
			j.attempts = append(j.attempts, attempts...)
			c.queue = append([]*job{j}, c.queue...)
			return
		}
	}
	c.record(j, *line.Entry, attempts)
}

// rejectReport stops a worker whose report cannot be trusted. The entry it
// held goes back to the queue once its output ends, like the entry of a
// worker that vanished.
func (c *coordinator) rejectReport(s *slot, reason string) {
	c.warn("device %s dropped: its worker sent an invalid report: %s", s.device.ID, reason)
	c.stop(s)
}

// record merges the attempts of earlier workers with the final report of
// the last one into the entry at the job's position.
func (c *coordinator) record(j *job, final Entry, attempts []Attempt) {
	all := append(append([]Attempt(nil), j.attempts...), attempts...)
	var priorMs int64
	for i := range all {
		all[i].Number = i + 1
	}
	for _, earlier := range j.attempts {
		priorMs += earlier.DurationMs
	}
	last := all[len(all)-1]
	final.Attempts = all
	final.Status = last.Status
	final.Error = last.Error
	final.DurationMs += priorMs
	c.results[j.index] = &final
}

// end settles a worker whose output ended. An entry it held but never
// reported goes back to the queue the first time.
func (c *coordinator) end(s *slot) {
	s.ended = true
	exit := s.worker.Wait()
	if j := s.current; j != nil {
		s.current = nil
		if j.requeues < requeueLimit && !c.interrupted {
			j.requeues++
			c.queue = append([]*job{j}, c.queue...)
		} else {
			c.abandon(j, true)
		}
	}
	if !s.ready && !c.interrupted {
		reason := s.dropReason
		if reason == "" {
			reason = fmt.Sprintf("its worker exited with code %d before it was ready: %s", exit.Code, strings.TrimSpace(exit.Stderr))
		}
		c.warn("device %s dropped: %s", s.device.ID, reason)
	}
}

// abandon records an entry no worker finished. It keeps the attempts that
// did run, and when vanished is true it adds a failed attempt for the
// worker that ended without reporting. An entry with no attempt at all
// always gets that failed attempt.
func (c *coordinator) abandon(j *job, vanished bool) {
	message := "the worker ended before it finished the entry"
	attempts := append([]Attempt(nil), j.attempts...)
	if vanished || len(attempts) == 0 {
		attempts = append(attempts, Attempt{Number: len(attempts) + 1, Status: statusFailed, Error: &message})
	}
	kind := j.kind
	if kind == "" {
		kind = "script"
	}
	last := attempts[len(attempts)-1]
	var total int64
	for _, a := range attempts {
		total += a.DurationMs
	}
	c.results[j.index] = &Entry{Path: j.path, Kind: kind, Status: last.Status, Error: last.Error, DurationMs: total, Attempts: attempts}
}

// dispatch hands the next path to every idle worker, or closes the input
// of idle workers once no entry is queued and none is running, since a
// running entry could still come back to the queue.
func (c *coordinator) dispatch() {
	if c.interrupted {
		return
	}
	running := false
	for _, s := range c.slots {
		running = running || s.current != nil
	}
	for _, s := range c.slots {
		if !s.ready || s.ended || s.lost || s.closing || s.current != nil {
			continue
		}
		switch {
		case len(c.queue) > 0:
			j := c.queue[0]
			if err := s.worker.Send(j.path); err != nil {
				s.lost = true
				c.warn("device %s dropped: its worker did not take %s: %v", s.device.ID, j.path, err)
				c.stop(s)
				continue
			}
			c.queue = c.queue[1:]
			s.current = j
			running = true
		case !running:
			s.closing = true
			_ = s.worker.CloseInput()
		}
	}
}

func (c *coordinator) outcome() Outcome {
	out := Outcome{Interrupted: c.interrupted}
	for _, s := range c.slots {
		if s.ready {
			out.Ready = append(out.Ready, s.device)
		}
	}
	for _, j := range c.queue {
		if len(j.attempts) > 0 {
			c.abandon(j, false)
		}
	}
	for i, result := range c.results {
		if result != nil {
			out.Entries = append(out.Entries, *result)
		} else {
			out.Unrun = append(out.Unrun, c.opts.Paths[i])
		}
	}
	if len(out.Unrun) > 0 && !c.interrupted {
		c.warn("%d entries were not run because every worker stopped", len(out.Unrun))
	}
	return out
}
