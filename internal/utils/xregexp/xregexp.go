// Package xregexp wraps regexp2 so a compiled pattern always carries a match
// timeout.
//
// regexp2 is a backtracking engine, unlike stdlib regexp. Without
// Regexp.MatchTimeout a catastrophic pattern backtracks without bound, so a
// single MatchString can pin a request goroutine indefinitely. The patterns here
// are operator-supplied (group match_regex, channel model filters, condition
// rules) and the strings matched against them are model names that arrive from
// upstream sites, so neither side is fully under our control.
//
// The timeout used to be set at each call site, and 4 of the 6 sites that
// actually matched had forgotten it:
//
//	internal/helper/channel.go       group match_regex vs channel models
//	internal/helper/fetch.go   (x2)  model-list filters
//	internal/op/group/auto.go        auto-group coverage check
//
// while internal/op/group/group.go and internal/relay/condition/evaluator.go set
// 250ms and a since-deleted copy in internal/op set 200ms — three different
// answers to one question. Compiling through this package makes forgetting
// impossible: there is no way to get a *regexp2.Regexp out of it without the
// timeout attached.
package xregexp

import (
	"time"

	"github.com/dlclark/regexp2"
)

// MatchTimeout bounds a single match attempt. 250ms is what the two call sites
// that had a timeout already used.
const MatchTimeout = 250 * time.Millisecond

// CompileECMAScript compiles pattern in ECMAScript mode with MatchTimeout set.
//
// Use this even where the result is only validated and discarded: it keeps every
// site honest about which engine and options are in play, and costs nothing.
func CompileECMAScript(pattern string) (*regexp2.Regexp, error) {
	re, err := regexp2.Compile(pattern, regexp2.ECMAScript)
	if err != nil {
		return nil, err
	}
	re.MatchTimeout = MatchTimeout
	return re, nil
}
