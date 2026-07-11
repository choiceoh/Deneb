package briefcase

import (
	"errors"
	"sync"
	"time"
)

var ErrClockRewind = errors.New("briefcase clock cannot move backwards")

// Clock is the sole time source used by Briefcase runtime components.
type Clock interface {
	Now() time.Time
}

// AdvancingClock is a Clock that a deterministic timeline may advance.
type AdvancingClock interface {
	Clock
	AdvanceTo(time.Time) error
}

// FrozenClock always returns the same instant.
type FrozenClock struct {
	now time.Time
}

func NewFrozenClock(at time.Time) *FrozenClock {
	return &FrozenClock{now: canonicalTime(at)}
}

func (c *FrozenClock) Now() time.Time {
	if c == nil {
		return time.Time{}
	}
	return c.now
}

// ManualClock advances only through explicit calls. Backward movement is
// rejected so a replay cannot silently make future information visible early.
type ManualClock struct {
	mu  sync.RWMutex
	now time.Time
}

func NewManualClock(at time.Time) *ManualClock {
	return &ManualClock{now: canonicalTime(at)}
}

func (c *ManualClock) Now() time.Time {
	if c == nil {
		return time.Time{}
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.now
}

func (c *ManualClock) Advance(delta time.Duration) error {
	if delta < 0 {
		return ErrClockRewind
	}
	if c == nil {
		return errors.New("briefcase: nil manual clock")
	}
	c.mu.Lock()
	c.now = c.now.Add(delta)
	c.mu.Unlock()
	return nil
}

func (c *ManualClock) AdvanceTo(at time.Time) error {
	if c == nil {
		return errors.New("briefcase: nil manual clock")
	}
	at = canonicalTime(at)
	c.mu.Lock()
	defer c.mu.Unlock()
	if at.Before(c.now) {
		return ErrClockRewind
	}
	c.now = at
	return nil
}

func canonicalTime(at time.Time) time.Time {
	// Round(0) strips the process-local monotonic reading. UTC removes location
	// pointer differences, making serialized and compared values reproducible.
	return at.Round(0).UTC()
}
