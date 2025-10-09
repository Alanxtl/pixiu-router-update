package model

import (
	"regexp"
	"sync"
	"sync/atomic"
)

import (
	"github.com/alanxtl/pixiu-router-update/new/trie"
	util "github.com/alanxtl/pixiu-router-update/utils"
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

var regexCache sync.Map // map[string]*regexp.Regexp

func getCachedRegexp(pat string) *regexp.Regexp {
	if v, ok := regexCache.Load(pat); ok {
		return v.(*regexp.Regexp)
	}
	// Compile 失败就返回 nil（调用方会忽略该正则）
	re, err := regexp.Compile(pat)
	if err != nil {
		return nil
	}
	if v, ok := regexCache.LoadOrStore(pat, re); ok {
		return v.(*regexp.Regexp)
	}
	return re
}

// -------- builder pools：构建期临时切片/对象的池化 --------
var compiledHeaderSlicePool = sync.Pool{
	New: func() any {
		s := make([]CompiledHeader, 0, 4) // 小容量起步，后续自动增长
		return &s
	},
}

func ToSnapshot(cfg *RouteConfiguration) *RouteSnapshot {
	// -------------- 预扫描：估算 header-only 数量，便于预分配 --------------
	headerOnlyCount := 0
	for _, r := range cfg.Routes {
		if r.Match.Path == "" && r.Match.Prefix == "" && len(r.Match.Headers) > 0 {
			headerOnlyCount++
		}
	}

	s := &RouteSnapshot{
		MethodTries: make(map[string]*trie.Trie, 8),
	}
	if headerOnlyCount > 0 {
		s.HeaderOnly = make([]HeaderRoute, 0, headerOnlyCount)
	}

	// 默认方法集合：常量切片，避免每次分配
	constMethods := []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "HEAD"}

	// 局部 get-or-create，减少 map 查询/分配噪音
	getTrie := func(m string) *trie.Trie {
		if t := s.MethodTries[m]; t != nil {
			return t
		}
		nt := trie.NewTrie()
		s.MethodTries[m] = &nt
		return &nt
	}

	for _, r := range cfg.Routes {
		// ============= A) header-only：with Headers, without Path / Prefix =============
		if r.Match.Path == "" && r.Match.Prefix == "" && len(r.Match.Headers) > 0 {
			hr := HeaderRoute{
				Methods: r.Match.Methods,
				Action:  r.Route,
			}

			// 用池获取一个临时切片来承接 headers，减少构建期垃圾
			chPtr := compiledHeaderSlicePool.Get().(*[]CompiledHeader)
			ch := (*chPtr)[:0] // reset

			for _, h := range r.Match.Headers {
				c := CompiledHeader{Name: h.Name}
				if h.Regex {
					// 1) 模型已提供编译好的正则（若有）→ 直接用
					if h.valueRE != nil {
						c.Regex = h.valueRE
					} else if len(h.Values) > 0 && h.Values[0] != "" {
						// 2) 否则走全局缓存/编译（跨快照复用）
						if re := getCachedRegexp(h.Values[0]); re != nil {
							c.Regex = re
						}
					}
				} else {
					// not regex → 枚举值拷贝
					if len(h.Values) > 0 {
						// 注意：这里直接 append 值字符串（不可变），无需复制底层数组
						c.Values = append(c.Values, h.Values...)
					}
				}
				ch = append(ch, c)
			}

			// 把临时切片的内容转移到快照（拥有期在快照）
			hr.Headers = make([]CompiledHeader, len(ch))
			copy(hr.Headers, ch)

			// 归还临时切片到池（清空引用，避免持有快照数据）
			*chPtr = (*chPtr)[:0]
			compiledHeaderSlicePool.Put(chPtr)

			s.HeaderOnly = append(s.HeaderOnly, hr)
			continue
		}

		// ================= B) Trie：精确/前缀/变量 路由 =================
		isPrefix := r.Match.Prefix != ""
		methods := r.Match.Methods
		if len(methods) == 0 {
			methods = constMethods // 使用常量切片，避免每次分配
		}
		for _, m := range methods {
			t := getTrie(m)
			key := util.GetTrieKeyWithPrefix(m, r.Match.Path, r.Match.Prefix, isPrefix)
			_, _ = t.Put(key, r.Route)
		}
	}
	return s
}
