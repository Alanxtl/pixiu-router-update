package new

import (
	"errors"
	"net/http"
	"regexp"
	"sync"
	"sync/atomic"
	"time"
)

import (
	"github.com/alanxtl/pixiu-router-update/new/model"
	"github.com/alanxtl/pixiu-router-update/new/trie"
	util "github.com/alanxtl/pixiu-router-update/utils"
)

type RouterCoordinator struct {
	active   snapshotHolder // atomic snapshot
	mu       sync.Mutex
	store    map[string]*model.Router
	timer    *time.Timer   // debounce timer
	debounce time.Duration // merge window, default 50ms
}

func CreateRouterCoordinator(routeConfig *model.RouteConfiguration) *RouterCoordinator {
	rc := &RouterCoordinator{
		store:    make(map[string]*model.Router),
		debounce: 50 * time.Millisecond, // merge window
	}
	// build initial config and store snapshot
	first := buildConfig(routeConfig.Routes)
	rc.active.store(toSnapshot(first))
	// copy initial routes to store
	for _, r := range routeConfig.Routes {
		rc.store[r.ID] = r
	}
	return rc
}

func (rm *RouterCoordinator) Route(req *http.Request) (*model.RouteAction, error) {
	s := rm.active.load()
	if s == nil {
		return nil, errors.New("router configuration is empty")
	}
	// header-only first
	for _, hr := range s.headerOnly {
		if !model.MethodAllowed(hr.methods, req.Method) {
			continue
		}
		if matchHeaders(hr.headers, req) {
			return &hr.action, nil
		}
	}
	// Trie
	t := s.methodTries[req.Method]
	if t == nil {
		return nil, errors.New("no route matched")
	}

	node, _, ok := t.Match(util.GetTrieKey(req.Method, req.URL.Path))
	if !ok || node == nil || node.GetBizInfo() == nil {
		return nil, errors.New("no route matched")
	}
	act := node.GetBizInfo().(model.RouteAction)
	return &act, nil
}

func (rm *RouterCoordinator) RouteByPathAndName(path, method string) (*model.RouteAction, error) {
	s := rm.active.load()
	if s == nil {
		return nil, errors.New("router configuration is empty")
	}
	t := s.methodTries[method]
	if t == nil {
		return nil, errors.New("no route matched")
	}
	node, _, ok := t.Match(util.GetTrieKey(method, path))
	if !ok || node == nil || node.GetBizInfo() == nil {
		return nil, errors.New("no route matched")
	}
	act := node.GetBizInfo().(model.RouteAction)
	return &act, nil
}

func (rm *RouterCoordinator) OnAddRouter(r *model.Router) {
	rm.mu.Lock()
	rm.store[r.ID] = r
	rm.schedulePublishLocked()
	rm.mu.Unlock()
}

func (rm *RouterCoordinator) OnDeleteRouter(r *model.Router) {
	rm.mu.Lock()
	delete(rm.store, r.ID)
	rm.schedulePublishLocked()
	rm.mu.Unlock()
}

// reset timer or publish directly
func (rm *RouterCoordinator) schedulePublishLocked() {
	if rm.debounce <= 0 {
		// fallback: immediate
		rm.publishLocked()
		return
	}
	if rm.timer == nil {
		rm.timer = time.NewTimer(rm.debounce)
		go rm.awaitAndPublish()
		return
	}
	// clear timer channel
	if !rm.timer.Stop() {
		select {
		case <-rm.timer.C:
		default:
		}
	}
	rm.timer.Reset(rm.debounce)
}

// wait for timer and publish
func (rm *RouterCoordinator) awaitAndPublish() {
	<-rm.timer.C
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.publishLocked()
	rm.timer = nil
}

// publish: clone from store -> build new config -> atomic switch
func (rm *RouterCoordinator) publishLocked() {
	// 1) clone routes
	next := make([]*model.Router, 0, len(rm.store))
	for _, r := range rm.store {
		next = append(next, r)
	}
	// 2) build new config
	cfg := buildConfig(next)
	// 3) atomic switch
	rm.active.store(toSnapshot(cfg))
}

func buildConfig(routes []*model.Router) *model.RouteConfiguration {
	cfg := &model.RouteConfiguration{
		RouteTrie: trie.NewTrie(),
		Routes:    make([]*model.Router, 0, len(routes)),
		Dynamic:   false,
	}
	for _, r := range routes {
		cfg.Routes = append(cfg.Routes, r)
	}
	initRegex(cfg)
	fillTrieFromRoutes(cfg)
	return cfg
}

func initRegex(cfg *model.RouteConfiguration) {
	for _, router := range cfg.Routes {
		headers := router.Match.Headers
		for i := range headers {
			if headers[i].Regex && len(headers[i].Values) > 0 {
				_ = headers[i].SetValueRegex(headers[i].Values[0])
			}
		}
	}
}

func fillTrieFromRoutes(cfg *model.RouteConfiguration) {
	for _, r := range cfg.Routes {
		methods := r.Match.Methods
		if len(methods) == 0 {
			methods = []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "HEAD"}
		}
		isPrefix := r.Match.Prefix != ""
		for _, m := range methods {
			key := getTrieKey(m, r.Match.Path, r.Match.Prefix, isPrefix)
			_, _ = cfg.RouteTrie.Put(key, r.Route)
		}
	}
}

func getTrieKey(method, path, prefix string, isPrefix bool) string {
	if isPrefix {
		if prefix != "" && prefix[len(prefix)-1] != '/' {
			prefix += "/"
		}
		prefix += "**"
		return util.GetTrieKey(method, prefix)
	}
	return util.GetTrieKey(method, path)
}

type routeSnapshot struct {
	methodTries map[string]*trie.Trie
	headerOnly  []headerRoute
}

type headerRoute struct {
	methods []string
	headers []compiledHeader
	action  model.RouteAction
}

type compiledHeader struct {
	name   string
	regex  *regexp.Regexp
	values []string
}

type snapshotHolder struct{ ptr atomic.Pointer[routeSnapshot] }

func (h *snapshotHolder) load() *routeSnapshot   { return h.ptr.Load() }
func (h *snapshotHolder) store(s *routeSnapshot) { h.ptr.Store(s) }

func matchHeaders(chs []compiledHeader, r *http.Request) bool {
	for _, ch := range chs {
		val := r.Header.Get(ch.name)
		if val == "" {
			return false
		}
		if ch.regex != nil {
			if !ch.regex.MatchString(val) {
				return false
			}
		} else if len(ch.values) > 0 {
			ok := false
			for _, v := range ch.values {
				if v == val {
					ok = true
					break
				}
			}
			if !ok {
				return false
			}
		}
	}
	return true
}

func toSnapshot(cfg *model.RouteConfiguration) *routeSnapshot {
	s := &routeSnapshot{
		methodTries: make(map[string]*trie.Trie, 8),
	}

	for _, r := range cfg.Routes {
		// header-only：with Headers, without Path / Prefix
		if r.Match.Path == "" && r.Match.Prefix == "" && len(r.Match.Headers) > 0 {
			hr := headerRoute{
				methods: r.Match.Methods,
				action:  r.Route,
			}
			for _, h := range r.Match.Headers {
				ch := compiledHeader{name: h.Name}
				if h.Regex {
					// use compiled regex
					if h.ValueRegex != nil {
						ch.regex = h.ValueRegex
					} else if len(h.Values) > 0 && h.Values[0] != "" {
						if re, err := regexp.Compile(h.Values[0]); err == nil {
							ch.regex = re
						}
					}
				} else {
					// not regex, use values
					if len(h.Values) > 0 {
						ch.values = append(ch.values, h.Values...)
					}
				}
				hr.headers = append(hr.headers, ch)
			}
			s.headerOnly = append(s.headerOnly, hr)
			continue
		}

		// Trie
		methods := r.Match.Methods
		if len(methods) == 0 {
			methods = []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "HEAD"}
		}
		isPrefix := r.Match.Prefix != ""
		for _, m := range methods {
			t := s.methodTries[m]
			if t == nil {
				nt := trie.NewTrie()
				t = &nt
				s.methodTries[m] = t
			}
			key := getTrieKey(m, r.Match.Path, r.Match.Prefix, isPrefix)
			_, _ = t.Put(key, r.Route)
		}
	}
	return s
}
