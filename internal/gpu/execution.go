//go:build gpu

package gpu

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/coalaura/onino/internal/search"
)

type execution struct {
	ctx         context.Context
	options     Options
	engine      *device
	worker      *verifier
	stats       *search.Stats
	metrics     *Metrics
	checked     *atomic.Uint64
	commands    []command
	candidates  chan<- discovered
	refills     <-chan feedback
	firstUpdate int
	lastUpdate  int
	seeded      int
	active      int
	inFlight    int
	nextSlot    int
	collectSlot int
	pending     uint32
	started     time.Time
	sampled     time.Time
	tail        time.Duration
	backlog     int
}

func (controller *execution) activate(streams int) error {
	for controller.seeded < streams {
		err := controller.ctx.Err()
		if err != nil {
			return err
		}

		index := controller.seeded

		state, update, err := newSeed(1)
		if err != nil {
			return err
		}

		controller.worker.seeds[index] = state
		controller.commands[index] = update
		controller.firstUpdate = min(controller.firstUpdate, index)
		controller.lastUpdate = max(index+1, min(controller.lastUpdate, controller.seeded))
		controller.seeded++
	}

	controller.active = streams

	return nil
}

func (controller *execution) total() uint64 {
	if controller.options.Monitor == nil {
		return controller.stats.Checked
	}

	return controller.options.Monitor.Snapshot().Checked
}

func (controller *execution) submit(configuration configuration, collectOnly bool) error {
	if !collectOnly {
		err := controller.ctx.Err()
		if err != nil {
			return err
		}
	}

	var (
		updates []command
		first   int
		last    int
	)

	if !collectOnly {
		controller.firstUpdate, controller.lastUpdate = drainUpdates(controller.refills, controller.commands, controller.firstUpdate, controller.lastUpdate)

		first = min(controller.firstUpdate, configuration.streams)
		last = min(controller.lastUpdate, configuration.streams)

		updates = controller.commands[first:last]
	}

	err := controller.engine.dispatch(controller.nextSlot, updates, first, configuration.streams, configuration.rounds, collectOnly)
	if err != nil {
		return err
	}

	if !collectOnly {
		clear(updates)

		controller.firstUpdate = max(controller.firstUpdate, last)
		if controller.firstUpdate == controller.lastUpdate {
			controller.firstUpdate = len(controller.commands)
			controller.lastUpdate = len(controller.commands)
		}
	}

	controller.metrics.Submissions++
	controller.inFlight++
	controller.nextSlot ^= 1

	if !collectOnly {
		controller.metrics.StopSubmitting = time.Since(controller.options.Started)
	}

	if controller.metrics.FirstWork == 0 && !collectOnly {
		controller.metrics.FirstWork = time.Since(controller.options.Started)

		if controller.options.Ready != nil {
			controller.options.Ready(controller.metrics.Device)
		}
	}

	return nil
}

func (controller *execution) collect() error {
	completed, err := controller.engine.collect(controller.collectSlot)
	if err != nil {
		return err
	}

	controller.inFlight--
	controller.collectSlot ^= 1
	controller.pending = completed.pending
	controller.stats.Checked += uint64(completed.checked)
	controller.checked.Store(controller.stats.Checked)

	metrics := controller.metrics
	metrics.Execution += completed.elapsed
	metrics.Gaps += completed.gap
	metrics.ReadBytes += uint64(16 + len(completed.hits)*24)
	metrics.ReadbackBytes += uint64(16 + controller.engine.capacity*24)
	metrics.Tail = max(metrics.Tail, completed.elapsed)
	metrics.GapTail = max(metrics.GapTail, completed.gap)
	metrics.Hits += uint64(len(completed.hits))
	metrics.Pending = max(metrics.Pending, completed.pending)
	metrics.Backlog = max(metrics.Backlog, len(controller.candidates))

	controller.tail = max(controller.tail, completed.elapsed)
	controller.backlog = max(controller.backlog, len(controller.candidates))

	found := time.Now()

	for _, result := range completed.hits {
		controller.candidates <- discovered{hit: result, found: found}
	}

	if controller.options.Diagnostic != nil && found.Sub(controller.sampled) >= time.Second {
		controller.engine.costs(metrics)
		controller.options.Diagnostic(fmt.Sprintf("sample t=%.6f cpu=%d gpu=%d submissions=%d execution_ns=%d gaps_ns=%d tail_ns=%d gap_tail_ns=%d copy_ns=%d record_ns=%d submit_ns=%d copied_bytes=%d read_bytes=%d readback_bytes=%d hits=%d pending=%d backlog=%d", found.Sub(controller.started).Seconds(), controller.total()-controller.stats.Checked, controller.stats.Checked, metrics.Submissions, metrics.Execution, metrics.Gaps, metrics.Tail, metrics.GapTail, metrics.Copy, metrics.Record, metrics.Submit, metrics.UploadBytes, metrics.ReadBytes, metrics.ReadbackBytes, metrics.Hits, metrics.Pending, metrics.Backlog))
		controller.sampled = found
	}

	return nil
}

// Drain never applies acknowledgements: a reported stream cannot resume and create
// more accepted work while cancellation or deactivation is draining its resident hits.
func (controller *execution) drain() error {
	for controller.inFlight > 0 || controller.pending > 0 {
		if controller.inFlight == 0 {
			err := controller.submit(configuration{streams: controller.active, rounds: 1}, true)
			if err != nil {
				return err
			}
		}

		err := controller.collect()
		if err != nil {
			return err
		}
	}

	return nil
}

func (controller *execution) measure(configuration configuration, duration time.Duration) (observation, error) {
	err := controller.activate(configuration.streams)
	if err != nil {
		return observation{}, err
	}

	controller.tail = 0
	controller.backlog = 0

	before := controller.total()
	checked := controller.stats.Checked
	hits := controller.metrics.Hits
	started := time.Now()

	for {
		err = controller.ctx.Err()
		if err != nil {
			return observation{}, err
		}

		// The first submission of every new configuration runs alone; only observed
		// bounded durations permit the second slot to overlap host coordination.
		limit := 1

		if controller.tail > 0 && controller.tail*2 < flightLimit && duration > 0 {
			limit = 2
		}

		for controller.inFlight < limit {
			err = controller.submit(configuration, false)
			if err != nil {
				return observation{}, err
			}
		}

		err = controller.collect()
		if err != nil {
			return observation{}, err
		}

		if controller.tail > flightLimit/2 || time.Since(started) >= duration {
			break
		}
	}

	err = controller.drain()
	if err != nil {
		return observation{}, err
	}

	observation := observation{
		configuration: configuration,
		rate:          float64(controller.total()-before) / time.Since(started).Seconds(),
		tail:          controller.tail,
		hits:          controller.metrics.Hits - hits,
		checked:       controller.stats.Checked - checked,
		backlog:       controller.backlog,
	}

	if controller.options.Diagnostic != nil {
		controller.options.Diagnostic(fmt.Sprintf("trial streams=%d rounds=%d combined_mps=%.3f tail_ms=%.3f checked=%d hits=%d backlog=%d", configuration.streams, configuration.rounds, observation.rate/1e6, float64(observation.tail)/1e6, observation.checked, observation.hits, observation.backlog))
	}

	if controller.tail > submissionLimit {
		return observation, errors.New("GPU validation submission exceeded 150ms; reduce the configuration")
	}

	return observation, nil
}

func (controller *execution) search(selected observation) error {
	configuration := selected.configuration

	err := controller.activate(configuration.streams)
	if err != nil {
		return err
	}

	controller.tail = selected.tail

	for controller.ctx.Err() == nil {
		congested := len(controller.candidates) > max(256, configuration.streams/2)
		if controller.tail > flightLimit/2 || (congested && (controller.options.AutoStreams || controller.options.AutoRounds)) {
			err = controller.drain()
			if err != nil {
				return err
			}

			configuration, err = reducedConfiguration(configuration, controller.options)
			if err != nil {
				return err
			}

			controller.active = configuration.streams
			controller.tail = 0
			controller.backlog = 0
			controller.metrics.Streams = configuration.streams
			controller.metrics.Rounds = configuration.rounds

			if controller.options.Diagnostic != nil {
				controller.options.Diagnostic(fmt.Sprintf("backoff streams=%d rounds=%d", configuration.streams, configuration.rounds))
			}
		}

		limit := 1

		if controller.tail > 0 && controller.tail*2 < flightLimit {
			limit = 2
		}

		if controller.inFlight < limit {
			err = controller.submit(configuration, false)
			if err != nil {
				return err
			}

			continue
		}

		err = controller.collect()
		if err != nil {
			return err
		}
	}

	return controller.ctx.Err()
}
