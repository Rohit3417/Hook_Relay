package circuitBreaker

import (
	"sync"
	"time"
)

type State string

const (
	Open     State = "open"
	Closed   State = "closed"
	HalfOpen State = "halfopen"
)

type endpointState struct {
	state               State
	consecutiveFailures int
	openedAt            time.Time // Used to check if cooldown has elapsed
	trialInFlight       bool      // In half open state only 1 request is to be checked
}

type Breaker struct {
	mu               sync.Mutex
	endpoints        map[string]*endpointState //Keyed By endpoint ID
	failureThreshold int
	cooldown         time.Duration
	now              func() time.Time
}

func New(failureThreshold int, cooldown time.Duration) *Breaker {
	endpoints := make(map[string]*endpointState) //Does not feed nil value, makes a empty map so program does not panic

	return &Breaker{endpoints: endpoints, failureThreshold: failureThreshold, cooldown: cooldown, now: time.Now}
}

func (b *Breaker) getOrCreate(endpointID string) *endpointState { //Helper function
	// Caller must hold b.mu
	rec, ok := b.endpoints[endpointID]
	if !ok {
		rec = &endpointState{state: Closed} // Other vlaues are by default 0 so we do not include them
		b.endpoints[endpointID] = rec
	}

	return rec
}

func (b *Breaker) Allow(endpointID string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	rec := b.getOrCreate(endpointID)
	currState := rec.state

	switch currState {
	case Closed:
		return true
	case Open:
		if b.now().Sub(b.endpoints[endpointID].openedAt) >= b.cooldown {

			rec.state = HalfOpen
			rec.trialInFlight = true
			return true
		} else {
			return false
		}
	default:
		return false
	}
}

func (b *Breaker) RecordResult(endpointID string, success bool) {
	b.mu.Lock()
	defer b.mu.Unlock()

	rec := b.getOrCreate(endpointID)
	currState := rec.state

	if success && currState == Closed {
		rec.consecutiveFailures = 0
	} else if !success && currState == Closed {
		rec.consecutiveFailures++
		if rec.consecutiveFailures >= b.failureThreshold {
			rec.state = Open
			rec.openedAt = b.now()
		}

	} else if success && currState == HalfOpen {
		rec.state = Closed
		rec.consecutiveFailures = 0
		rec.trialInFlight = false
	} else if !success && currState == HalfOpen {
		rec.state = Open
		rec.openedAt = b.now()
	}

}
