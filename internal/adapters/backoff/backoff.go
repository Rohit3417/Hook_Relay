package backoff

import (
	"math"
	"math/rand/v2"
	"time"
)

/* without jitter, if a destination server goes down and you have many events queued for it,
all your retries would land at the exact same moments (1 min, 5 min, 30 min...) —
 a "thundering herd" that hammers the destination the instant it comes back up. Jitter spreads that out.
*/

// waitTime is a exponential growth function
// Formula used : Wait Time = min(MaxWait, Base * 2^attempt) +_ Random Jitter
// Standalone function just like out signer and ssrf

// MaxWait is what we allow other wise it can go very high
func Wait(attempt int, base time.Duration, maxWait time.Duration) time.Duration {
	exponential := base * time.Duration(math.Pow(2, float64(attempt)))
	capped := min(maxWait, exponential)

	if capped <= 0 {
		return 0
	}

	// Generating random between 0 and capped time
	return time.Duration(rand.Int64N(int64(capped)))
}
