package billingexpr

import (
	"fmt"
	"math"
)

// MaxQuota / MinQuota define the representable range for quota values.
// Quota columns in the database are 32-bit integers; exceeding this range
// must clamp instead of wrapping (which would turn a charge into a credit).
const (
	MaxQuota = math.MaxInt32
	MinQuota = math.MinInt32
)

// QuotaClampKind identifies why a quota conversion had to be saturated.
type QuotaClampKind string

const (
	QuotaClampOverflow  QuotaClampKind = "overflow"
	QuotaClampUnderflow QuotaClampKind = "underflow"
	QuotaClampNaN       QuotaClampKind = "nan"
)

// QuotaClamp describes a single saturation event during quota conversion.
// It is returned alongside the clamped value so callers (e.g. settlement,
// logging) can record the anomaly for auditing.
type QuotaClamp struct {
	Op       string         `json:"op"` // e.g. "QuotaRound", "QuotaFromFloat"
	Kind     QuotaClampKind `json:"kind"`
	Original float64        `json:"original"` // best-effort original value
	Clamped  int            `json:"clamped"`  // the saturated result actually used
}

// Error lets QuotaClamp be used as an error for strict paths.
func (c *QuotaClamp) Error() string {
	if c == nil {
		return ""
	}
	return fmt.Sprintf("quota conversion (%s) %s: original=%g, clamped=%d", c.Op, c.Kind, c.Original, c.Clamped)
}

// AuditMap renders the clamp as a generic map for embedding into logs
// (e.g. under admin_info.quota_saturation or similar).
func (c *QuotaClamp) AuditMap() map[string]interface{} {
	if c == nil {
		return nil
	}
	return map[string]interface{}{
		"op":       c.Op,
		"kind":     c.Kind,
		"original": c.Original,
		"clamped":  c.Clamped,
	}
}

// saturateQuota converts a float64 to int while clamping to int32 range.
// Returns the clamped integer and a non-nil *QuotaClamp when saturation occurred.
func saturateQuota(value float64, op string) (int, *QuotaClamp) {
	switch {
	case math.IsNaN(value):
		clamp := &QuotaClamp{Op: op, Kind: QuotaClampNaN, Original: value, Clamped: 0}
		return clamp.Clamped, clamp
	case value >= MaxQuota:
		clamp := &QuotaClamp{Op: op, Kind: QuotaClampOverflow, Original: value, Clamped: MaxQuota}
		return clamp.Clamped, clamp
	case value <= MinQuota:
		clamp := &QuotaClamp{Op: op, Kind: QuotaClampUnderflow, Original: value, Clamped: MinQuota}
		return clamp.Clamped, clamp
	default:
		return int(value), nil
	}
}

// QuotaRound converts a float64 quota value to int using half-away-from-zero
// rounding, with saturation to the int32 range.
//
// Every tiered billing path (pre-consume, settlement, log fields) SHOULD use
// this (or QuotaRoundChecked) to avoid +-1 discrepancies and wraparound bugs.
func QuotaRound(value float64) int {
	quota, _ := QuotaRoundChecked(value)
	return quota
}

// QuotaRoundChecked is like QuotaRound but also returns a non-nil *QuotaClamp
// when the value was clamped, allowing callers to audit the event.
func QuotaRoundChecked(value float64) (int, *QuotaClamp) {
	return saturateQuota(math.Round(value), "QuotaRound")
}

// QuotaFromFloat converts a computed quota value to int, truncating toward zero,
// with saturation. Use for float products of prices, ratios, and user-controlled
// multipliers.
func QuotaFromFloat(value float64) int {
	quota, _ := QuotaFromFloatChecked(value)
	return quota
}

// QuotaFromFloatChecked is QuotaFromFloat but also returns a non-nil *QuotaClamp
// when clamping occurred.
func QuotaFromFloatChecked(value float64) (int, *QuotaClamp) {
	return saturateQuota(value, "QuotaFromFloat")
}
