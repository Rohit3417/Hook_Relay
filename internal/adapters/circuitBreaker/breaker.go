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
		if b.now().Sub(rec.openedAt) >= b.cooldown {
			rec.state = HalfOpen
			rec.trialInFlight = true
			return true
		}
		return false
	case HalfOpen:
		if rec.trialInFlight {
			return false
		}
		rec.trialInFlight = true
		return true
	default:
		return false
	}
}

func (b *Breaker) RecordResult(endpointID string, success bool) bool {
	b.mu.Lock()
	defer b.mu.Unlock()

	rec := b.getOrCreate(endpointID)

	if success {
		rec.consecutiveFailures = 0
		rec.state = Closed
		rec.trialInFlight = false
		return false
	}

	// Failure case
	rec.consecutiveFailures++
	if rec.state == Closed && rec.consecutiveFailures >= b.failureThreshold {
		rec.state = Open
		rec.openedAt = b.now()
		rec.trialInFlight = false
		return true // Just tripped to open
	} else if rec.state == HalfOpen {
		rec.state = Open
		rec.openedAt = b.now()
		rec.trialInFlight = false
		return true
	}

	return false
}

func (b *Breaker) GetState(endpointID string) State {
	b.mu.Lock()
	defer b.mu.Unlock()

	rec := b.getOrCreate(endpointID)
	return rec.state
}
