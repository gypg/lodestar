package billingexpr

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestQuotaRound_ClampOnOverflow ensures that an oversized quota value clamps
// to MaxInt32 instead of wrapping to a negative number.
func TestQuotaRound_ClampOnOverflow(t *testing.T) {
	huge := float64(math.MaxInt32) * 10
	q, clamp := QuotaRoundChecked(huge)

	assert.Equal(t, MaxQuota, q)
	require.NotNil(t, clamp)
	assert.Equal(t, QuotaClampOverflow, clamp.Kind)
	assert.Equal(t, MaxQuota, clamp.Clamped)
}

// TestQuotaRound_ClampOnUnderflow ensures negative overflow clamps to MinInt32.
func TestQuotaRound_ClampOnUnderflow(t *testing.T) {
	negativeHuge := float64(math.MinInt32) * 10
	q, clamp := QuotaRoundChecked(negativeHuge)

	assert.Equal(t, MinQuota, q)
	require.NotNil(t, clamp)
	assert.Equal(t, QuotaClampUnderflow, clamp.Kind)
	assert.Equal(t, MinQuota, clamp.Clamped)
}

// TestQuotaRound_NoClampInRange confirms normal values return no clamp.
func TestQuotaRound_NoClampInRange(t *testing.T) {
	q, clamp := QuotaRoundChecked(12345.67)
	assert.Equal(t, 12346, q) // math.Round half-away-from-zero
	assert.Nil(t, clamp)
}

// TestQuotaRound_NaN clamps NaN to 0 with QuotaClampNaN.
func TestQuotaRound_NaN(t *testing.T) {
	q, clamp := QuotaRoundChecked(math.NaN())
	assert.Equal(t, 0, q)
	require.NotNil(t, clamp)
	assert.Equal(t, QuotaClampNaN, clamp.Kind)
}

// TestQuotaFromFloat_Clamp behaves the same for float truncation path.
func TestQuotaFromFloat_Clamp(t *testing.T) {
	huge := float64(math.MaxInt32) + 999999
	q, clamp := QuotaFromFloatChecked(huge)
	assert.Equal(t, MaxQuota, q)
	require.NotNil(t, clamp)
	assert.Equal(t, QuotaClampOverflow, clamp.Kind)
}
