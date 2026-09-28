package circuitbreaker

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

	b.endpoints[endpointID] = b.getOrCreate(endpointID)
	currState := b.endpoints[endpointID].state

	switch currState {
	case Closed:
		return true
	case Open:
		if b.now().Sub(b.endpoints[endpointID].openedAt) >= b.cooldown {

			b.endpoints[endpointID].state = HalfOpen
			b.endpoints[endpointID].trialInFlight = true
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

	b.endpoints[endpointID] = b.getOrCreate(endpointID)
	currState := b.endpoints[endpointID].state

	if success && currState == Closed {
		b.endpoints[endpointID].consecutiveFailures = 0
	} else if !success && currState == Closed {
		b.endpoints[endpointID].consecutiveFailures++
		if b.endpoints[endpointID].consecutiveFailures >= b.failureThreshold {
			b.endpoints[endpointID].state = Open
			b.endpoints[endpointID].openedAt = b.now()
		}

	} else if success && currState == HalfOpen {
		b.endpoints[endpointID].state = Closed
		b.endpoints[endpointID].consecutiveFailures = 0
		b.endpoints[endpointID].trialInFlight = false
	} else if !success && currState == HalfOpen {
		b.endpoints[endpointID].state = Open
		b.endpoints[endpointID].openedAt = b.now()
	}

}
