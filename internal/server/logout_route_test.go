package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

/*
POST /api/v1/user/logout must stay OUTSIDE any Auth()-gated group.

A client holding a stale, expired or malformed token has to be able to wipe its
own cookie. extractToken reads the cookie BEFORE the Authorization header, so a
cookie left in place keeps authorizing every later request as the previous user
until the JWT TTL elapses — up to 90 days under "remember me". If logout sat
behind Auth(), the very users who most need it (bad token) could not call it.

★ Why this test lives here and reads source rather than driving a request:
handlers/user_logout_test.go mounts logout on its own gin engine, so it verifies
the HANDLER, not the production registration — wrapping the real route with
Auth() leaves every test in that file green (verified: that mutation survives).
Gin also exposes only the final handler per route, not the middleware chain, so
the gate is not observable from the registered engine. Hence a source assertion
on the group that registers "/logout".
*/
func TestLogoutRouteIsNotAuthGated(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("handlers", "user.go"))
	if err != nil {
		t.Fatalf("read handlers/user.go: %v", err)
	}
	text := string(src)

	if !strings.Contains(text, `"/logout"`) {
		t.Fatal(`handlers/user.go registers no "/logout" route`)
	}

	// Walk every group-router block and find the one registering /logout.
	const marker = "router.NewGroupRouter("
	blocks := strings.Split(text, marker)
	var checked int
	for i, b := range blocks[1:] {
		// Trim at the next group so a later group's Auth() is not attributed here.
		if end := strings.Index(b, marker); end != -1 {
			b = b[:end]
		}
		if !strings.Contains(b, `"/logout"`) {
			continue
		}
		checked++
		if strings.Contains(b, "middleware.Auth()") {
			t.Fatalf("group #%d registers /logout and also applies middleware.Auth(); "+
				"a client whose token is already invalid could then never clear its own "+
				"cookie, and extractToken keeps honouring that cookie for the rest of the "+
				"JWT TTL (up to 90 days with remember-me). block=%q", i+1, b)
		}
	}

	// /logout may also be attached to a pre-built public group variable rather than
	// inline. Cover that shape too: locate the variable it is added to and assert
	// that variable's own construction carries no Auth().
	if checked == 0 {
		idx := strings.Index(text, `"/logout"`)
		head := text[:idx]
		// The nearest preceding `X.AddRoute(` tells us which group it joined.
		addIdx := strings.LastIndex(head, ".AddRoute(")
		if addIdx == -1 {
			t.Fatal(`could not determine which router group registers "/logout"`)
		}
		lineStart := strings.LastIndex(head[:addIdx], "\n") + 1
		groupVar := strings.TrimSpace(head[lineStart:addIdx])
		if groupVar == "" {
			t.Fatal(`could not name the router group variable for "/logout"`)
		}
		// Find where that variable is assigned and inspect its chain.
		assign := groupVar + " :="
		aIdx := strings.Index(text, assign)
		if aIdx == -1 {
			t.Fatalf("router group variable %q for /logout is never assigned in this file", groupVar)
		}
		chain := text[aIdx:]
		if end := strings.Index(chain, "\n\n"); end != -1 {
			chain = chain[:end]
		}
		if strings.Contains(chain, "middleware.Auth()") {
			t.Fatalf("router group %q carries middleware.Auth() and is the group that "+
				"registers /logout; a stale token could then never clear its cookie. chain=%q",
				groupVar, chain)
		}
		checked++
	}

	if checked == 0 {
		t.Fatal(`no router group was matched for "/logout" — the guard did not actually inspect anything`)
	}
}
