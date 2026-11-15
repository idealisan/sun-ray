//go:build !nogui && !webui

package main

// policy_match.go is the GUI's thin entry to the shared policy test. The
// matching logic itself lives in policy_shared.go so the webui uses exactly
// the same rules, with the GUI authoritative.

// matchPolicy answers the test window for the GUI's current config.
func (a *app) matchPolicy(input string) (policyMatch, error) {
	return matchPolicyFor(&a.cfg.Global, input)
}
