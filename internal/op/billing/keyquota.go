package billing

/*
Per-key quota admission bound (octopus #234).

The MaxCost check in the auth middleware reads an in-memory stats snapshot and
settlement happens after the response — a pure check-then-act. N concurrent
requests on one key all pass the gate before the first one settles, so the key
spends up to N x (one request's cost) past its limit.

Same philosophy as AcquireForKey (inflight.go): an admission rule, not a
pre-charge. Counters are per-process and in-memory; Lodestar runs
single-instance. A leaked release only over-restricts one key until restart.

	headroom = MaxCost - usedCost   (used = settled usage)
	admit iff headroom > 0 AND inflight * est < headroom

est is the assumed worst-case per-request cost, taken from the same
max_expected_request_cost knob the wallet gate uses. When the knob is unset,
est falls back to the key's entire remaining headroom — a quota-capped key
under concurrent traffic then serializes (safe default; raise the knob to
admit bursts). With the knob set, this bound is one knob-value per in-flight
request, identical in shape to the wallet bound.
*/

import "sync"

var (
	keyQuotaMu sync.Mutex
	// inflightByKey counts admitted-but-not-released requests per API key.
	// Entries are deleted at zero so an idle instance holds nothing.
	inflightByKey = make(map[int]int)
)

// QuotaAdmit decides whether a request on this key may proceed under its
// MaxCost bound, reserving an in-flight slot if so. The returned release MUST
// be deferred by the caller on every exit path. Keys without a MaxCost cap
// are admitted unconditionally (their bound stays at settlement as before).
//
// usedCost is the key's settled usage cost — pass the same snapshot the
// static check in the auth middleware read, so both gates agree.
func QuotaAdmit(keyID int, maxCost float64, usedCost float64) (release func(), ok bool) {
	if maxCost <= 0 {
		return noopRelease, true
	}
	headroom := maxCost - usedCost
	if headroom <= 0 {
		return noopRelease, false
	}

	est := maxExpectedRequestCost()
	if est <= 0 {
		est = headroom
	}

	keyQuotaMu.Lock()
	defer keyQuotaMu.Unlock()
	inflight := inflightByKey[keyID]
	if float64(inflight)*est >= headroom {
		return noopRelease, false
	}
	inflightByKey[keyID] = inflight + 1

	var once sync.Once
	return func() {
		once.Do(func() {
			keyQuotaMu.Lock()
			defer keyQuotaMu.Unlock()
			if n := inflightByKey[keyID] - 1; n > 0 {
				inflightByKey[keyID] = n
			} else {
				delete(inflightByKey, keyID)
			}
		})
	}, true
}

// InflightForKey reports the current in-flight count for one key. Test-only.
func InflightForKey(keyID int) int {
	keyQuotaMu.Lock()
	defer keyQuotaMu.Unlock()
	return inflightByKey[keyID]
}
