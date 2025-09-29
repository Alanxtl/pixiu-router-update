package router

import (
	"errors"
	"net/http"

	"github.com/alanxtl/pixiu-router-update/new/model"
	"github.com/alanxtl/pixiu-router-update/new/trie"
	util "github.com/alanxtl/pixiu-router-update/utils"
)

// —— 保持对外可见的类型/方法签名不变 ——

// RouterCoordinator：外部可见字段保持不变（若原来有），新增的 holder 为私有字段
type RouterCoordinator struct {
	active model.SnapshotHolder     // 当前活动快照（原子指针）
	store  map[string]*model.Router // 权威路由表（仅用于重建，按需保留）
}

// CreateRouterCoordinator：签名不变
func CreateRouterCoordinator(routeConfig *model.RouteConfiguration) *RouterCoordinator {
	rc := &RouterCoordinator{
		store: make(map[string]*model.Router),
	}
	// 基于入参 Routes 构建并发布首个快照
	first := buildConfig(routeConfig.Routes)
	rc.active.Store(toSnapshot(first))
	// 保存权威路由副本（用于增删重建）
	for _, r := range routeConfig.Routes {
		rc.store[r.ID] = r
	}
	return rc
}

// Route：签名不变；读路径无锁（只读快照）
func (rm *RouterCoordinator) Route(req *http.Request) (*model.RouteAction, error) {
	s := rm.active.Load()
	if s == nil {
		return nil, errors.New("router configuration is empty")
	}
	// 1) header-only 优先
	for _, hr := range s.HeaderOnly {
		if !model.MethodAllowed(hr.Methods, req.Method) {
			continue
		}
		if model.MatchHeaders(hr.Headers, req) {
			return &hr.Action, nil
		}
	}
	// 2) 方法分区 Trie 匹配
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

// RouteByPathAndName：签名不变；内部走当前活动快照
func (rm *RouterCoordinator) RouteByPathAndName(path, method string) (*model.RouteAction, error) {
	s := rm.active.Load()
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

// OnAddRouter：签名不变；不再运行期写 Trie，改为重建并原子切换
func (rm *RouterCoordinator) OnAddRouter(r *model.Router) {
	if rm.store == nil {
		rm.store = make(map[string]*model.Router)
	}
	rm.store[r.ID] = r
	rm.reloadFromStore()
}

// OnDeleteRouter：签名不变
func (rm *RouterCoordinator) OnDeleteRouter(r *model.Router) {
	if rm.store == nil {
		return
	}
	delete(rm.store, r.ID)
	rm.reloadFromStore()
}

// —— 内部：从权威表重建 RouteConfiguration/快照并发布 ——

// 保持你现有 RouteConfiguration 结构和行为；这里只在“构建期”写入 Trie
func buildConfig(routes []*model.Router) *model.RouteConfiguration {
	cfg := &model.RouteConfiguration{
		RouteTrie: trie.NewTrie(), // 若你的 RouteConfiguration 里是 triepkg.Trie，改成那一版
		Routes:    make([]*model.Router, 0, len(routes)),
		Dynamic:   false,
	}
	for _, r := range routes {
		cfg.Routes = append(cfg.Routes, r)
	}
	initRegex(cfg)          // 仍按旧逻辑预编译 headers 的正则
	fillTrieFromRoutes(cfg) // 只在构建期写 Trie
	return cfg
}

func initRegex(cfg *model.RouteConfiguration) {
	for _, router := range cfg.Routes {
		headers := router.Match.Headers
		for i := range headers {
			if headers[i].Regex && len(headers[i].Values) > 0 {
				_ = headers[i].SetValueRegex(headers[i].Values[0]) // 只编译首个值
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

// 兼容旧有的 key 规则（Prefix -> **）
func getTrieKey(method, path, prefix string, isPrefix bool) string {
	if isPrefix {
		p := prefix
		if p != "" && p[len(p)-1] != '/' {
			p += "/"
		}
		p += "**"
		return util.GetTrieKey(method, p)
	}
	return util.GetTrieKey(method, path)
}

// 将 RouteConfiguration 转为 routeSnapshot（方法分区的多棵 Trie + header-only 预编译）
func toSnapshot(cfg *model.RouteConfiguration) *model.RouteSnapshot {
	s := &model.RouteSnapshot{
		MethodTries: make(map[string]*trie.Trie, 8),
	}

	// 1) 从 cfg.RouteTrie 读取所有条目，按方法分流（若你的 Trie 没有遍历 API，可在构建期直接分方法写入）
	// 这里演示简单做法：在 fillTrieFromRoutes 时就分方法放入 s.methodTries
	// 如果你更想“单棵 Trie”，也可保持单棵，这里只要读路径无锁即可。

	// 简单方案：重建一遍（与 buildSnapshot 等价）
	for _, r := range cfg.Routes {
		if r.Match.Path == "" && r.Match.Prefix == "" && len(r.Match.Headers) > 0 {
			hr := model.HeaderRoute{
				Methods: r.Match.Methods,
				Action:  r.Route,
			}
			for _, h := range r.Match.Headers {
				ch := model.CompiledHeader{Name: h.Name}
				if h.Regex && h.ValueRegex != nil {
					ch.Regex = h.ValueRegex
				} else {
					ch.Values = append(ch.Values, h.Values...)
				}
				hr.Headers = append(hr.Headers, ch)
			}
			s.HeaderOnly = append(s.HeaderOnly, hr)
			continue
		}
		methods := r.Match.Methods
		if len(methods) == 0 {
			methods = []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "HEAD"}
		}
		isPrefix := r.Match.Prefix != ""
		for _, m := range methods {
			t := s.MethodTries[m]
			if t == nil {
				nt := trie.NewTrie()
				t = &nt
				s.MethodTries[m] = t
			}
			key := getTrieKey(m, r.Match.Path, r.Match.Prefix, isPrefix)
			_, _ = t.Put(key, r.Route)
		}
	}
	return s
}

func (rm *RouterCoordinator) reloadFromStore() {
	// 将权威表拍成切片
	next := make([]*model.Router, 0, len(rm.store))
	for _, r := range rm.store {
		next = append(next, r)
	}
	cfg := buildConfig(next)         // 构建期写
	rm.active.Store(toSnapshot(cfg)) // 原子发布
}
