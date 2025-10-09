package new

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

import (
	"github.com/alanxtl/pixiu-router-update/new/model"
	"github.com/alanxtl/pixiu-router-update/new/trie"
	util "github.com/alanxtl/pixiu-router-update/utils"
)

// RouterCoordinator the router coordinator for http connection manager
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
	rc.active.store(model.ToSnapshot(first))
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
	for _, hr := range s.HeaderOnly {
		if !model.MethodAllowed(hr.Methods, req.Method) {
			continue
		}
		if matchHeaders(hr.Headers, req) {
			return &hr.Action, nil
		}
	}
	// Trie
	t := s.MethodTries[req.Method]
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
	t := s.MethodTries[method]
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
	rm.active.store(model.ToSnapshot(cfg))
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
				if err := headers[i].SetValueRegex(headers[i].Values[0]); err != nil {
					// todo use logger
					fmt.Printf("invalid regexp in headers[%d]: %v", i, err)
				}
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
			key := util.GetTrieKeyWithPrefix(m, r.Match.Path, r.Match.Prefix, isPrefix)
			_, _ = cfg.RouteTrie.Put(key, r.Route)
		}
	}
}

type snapshotHolder struct {
	ptr atomic.Pointer[model.RouteSnapshot]
}

func (h *snapshotHolder) load() *model.RouteSnapshot   { return h.ptr.Load() }
func (h *snapshotHolder) store(s *model.RouteSnapshot) { h.ptr.Store(s) }

func matchHeaders(chs []model.CompiledHeader, r *http.Request) bool {
	for _, ch := range chs {
		val := r.Header.Get(ch.Name)
		if val == "" {
			return false
		}
		if ch.Regex != nil {
			if !ch.Regex.MatchString(val) {
				return false
			}
		} else if len(ch.Values) > 0 {
			ok := false
			for _, v := range ch.Values {
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
