package module

import (
	"sync"
	"time"

	"github.com/diamondburned/arikawa/v3/discord"
)

type messageThrottleConfig struct {
	max int
	do  func(discord.ChannelID, []discord.MessageID)
}

type messageThrottlers struct {
	m sync.Map
	c messageThrottleConfig
}

func newMessageThrottlers(config messageThrottleConfig) *messageThrottlers {
	return &messageThrottlers{c: config}
}

func (ts *messageThrottlers) forChannel(id discord.ChannelID) *messageThrottler {
	v, ok := ts.m.Load(id)
	if ok {
		return v.(*messageThrottler)
	}

	v, _ = ts.m.LoadOrStore(id, newMessageThrottler(ts.c, id))
	return v.(*messageThrottler)
}

type messageThrottler struct {
	queue   []discord.MessageID
	queueMu sync.Mutex

	timer struct {
		sync.Mutex
		reset chan time.Duration
	}

	config messageThrottleConfig
	chID   discord.ChannelID
}

func newMessageThrottler(config messageThrottleConfig, chID discord.ChannelID) *messageThrottler {
	return &messageThrottler{
		config: config,
		chID:   chID,
	}
}

// AddMessage adds a message to the queue. The message will be dispatched after
// the delay time.
func (t *messageThrottler) AddMessage(id discord.MessageID, delayDuration time.Duration) {
	var overflow []discord.MessageID

	t.queueMu.Lock()
	// Check for overflowing queue. If we overflow, then we'll send them off
	// right away.
	if len(t.queue) >= t.config.max {
		overflow = t.queue
		t.queue = []discord.MessageID{id}
	} else {
		t.queue = append(t.queue, id)
	}
	t.queueMu.Unlock()

	t.tryStartJob(delayDuration)

	if len(overflow) > 0 {
		go t.config.do(t.chID, overflow)
	}
}

// DelaySending adds into the current delay time. It delays the callback to
// allow the queue to accumulate more messages.
func (t *messageThrottler) DelaySending(delayDuration time.Duration) {
	// Exit if we have nothing in the queue.
	t.queueMu.Lock()
	queueLen := len(t.queue)
	t.queueMu.Unlock()

	if queueLen == 0 {
		return
	}

	t.tryStartJob(delayDuration)
}

func (t *messageThrottler) tryStartJob(delay time.Duration) {
	t.timer.Lock()
	defer t.timer.Unlock()

	if t.timer.reset == nil {
		t.timer.reset = make(chan time.Duration, 1)
	}

	// Already started. Just send the delay over. We'll just try and send
	// the duration, however if that doesn't immediately work, we'll just
	// spawn a new goroutine to do our job.
	select {
	case t.timer.reset <- delay:
		return
	case <-t.timer.reset:
		// If this case ever hits, then the worker is probably waiting for
		// the mutex to unlock. We should be able to just spawn a new
		// goroutine with the current delay.
	}

	go func() {
		timer := time.NewTimer(delay)
		for {
			select {
			case d := <-t.timer.reset:
				if !timer.Stop() {
					<-timer.C
				}
				timer.Reset(d)

			case <-timer.C:
				// Steal the queue.
				t.queueMu.Lock()
				queue := t.queue
				t.queue = nil
				t.queueMu.Unlock()

				// Do the action.
				t.config.do(t.chID, queue)
				return
			}
		}
	}()
}
