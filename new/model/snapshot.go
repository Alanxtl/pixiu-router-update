package model

import (
	"regexp"
	"sync/atomic"
)

import (
	"github.com/alanxtl/pixiu-router-update/new/trie"
)

// RouteSnapshot Read-only snapshot for routing
type RouteSnapshot struct {
	// multi-trie for each method, built once and read-only
	MethodTries map[string]*trie.Trie

	// precompiled regex for header-only routes
	HeaderOnly []HeaderRoute
}

type HeaderRoute struct {
	Methods []string
	Headers []CompiledHeader
	Action  RouteAction
}

type CompiledHeader struct {
	Name   string
	Regex  *regexp.Regexp
	Values []string
}

// SnapshotHolder holds current active snapshot
type SnapshotHolder struct {
	ptr atomic.Pointer[RouteSnapshot]
}

func (h *SnapshotHolder) Load() *RouteSnapshot   { return h.ptr.Load() }
func (h *SnapshotHolder) Store(s *RouteSnapshot) { h.ptr.Store(s) }

func MethodAllowed(methods []string, m string) bool {
	if len(methods) == 0 {
		return true
	}
	for _, x := range methods {
		if x == m {
			return true
		}
	}
	return false
}
