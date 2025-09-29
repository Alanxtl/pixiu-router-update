package model

import (
	"net/http"
	"regexp"
	"sync/atomic"
)

import (
	"github.com/alanxtl/pixiu-router-update/new/trie"
	utils "github.com/alanxtl/pixiu-router-update/utils"
)

// 说明：下面用到的类型（RouteAction、Router、RouterMatch、Trie）
// 全部使用你现有的导出结构，不做修改。这里假定它们在同一个包或子包。
// 若在其他包（例如 model、trie），请自行调整 import。

// 内部的、不可变的路由快照
type RouteSnapshot struct {
	// 分方法的多棵 Trie（构建完成后只读）
	MethodTries map[string]*trie.Trie

	// 只依赖 Header 的规则（预编译正则）
	HeaderOnly []HeaderRoute
}

type HeaderRoute struct {
	Methods []string
	Headers []CompiledHeader
	Action  RouteAction
}

type CompiledHeader struct {
	Name   string
	Regex  *regexp.Regexp // 如果是正则 Header，提前编译
	Values []string       // 非正则值集
}

// 当前活动快照（原子切换）
type SnapshotHolder struct {
	ptr atomic.Pointer[RouteSnapshot]
}

func (h *SnapshotHolder) Load() *RouteSnapshot   { return h.ptr.Load() }
func (h *SnapshotHolder) Store(s *RouteSnapshot) { h.ptr.Store(s) }

// —— 工具：方法允许判断（保持行为不变）
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

func MatchHeaders(chs []CompiledHeader, r *http.Request) bool {
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

// BuildSnapshot：把“权威配置/内存表中的路由切片”一次性构建为不可变快照
func buildSnapshot(routes []*Router) *RouteSnapshot {
	s := &RouteSnapshot{
		MethodTries: make(map[string]*trie.Trie, 8),
	}

	for _, r := range routes {
		// 1) 只 header 的路由（无 path/prefix），集中到 headerOnly，预编译正则
		if r.Match.Path == "" && r.Match.Prefix == "" && len(r.Match.Headers) > 0 {
			hr := HeaderRoute{
				Methods: r.Match.Methods,
				Action:  r.Route,
			}
			for _, h := range r.Match.Headers {
				ch := CompiledHeader{Name: h.Name}
				if h.Regex && len(h.Values) > 0 {
					if re, err := regexp.Compile(h.Values[0]); err == nil {
						ch.Regex = re
					}
				} else {
					ch.Values = append(ch.Values, h.Values...)
				}
				hr.Headers = append(hr.Headers, ch)
			}
			s.HeaderOnly = append(s.HeaderOnly, hr)
			continue
		}

		// 2) 带 path / prefix 的路由：按 HTTP 方法分区，每个方法一棵 Trie
		methods := r.Match.Methods
		if len(methods) == 0 {
			// 若你已有默认方法集，保持一致；否则按需列出
			methods = []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "HEAD"}
		}
		for _, m := range methods {
			t := s.MethodTries[m]
			if t == nil {
				nt := trie.NewTrie() // 你现有的 Trie 构造，不改 API
				t = &nt
				s.MethodTries[m] = t
			}
			key := buildTrieKey(m, r.Match) // 与现有 getTrieKey 规则一致
			_, _ = t.Put(key, r.Route)      // 构建阶段写入（发布后只读）
		}
	}
	return s
}

// 跟你现有 getTrieKey 行为保持一致：
// - 有 Prefix：规范化为 "prefix/**"
// - 否则使用 Path
func buildTrieKey(method string, m RouterMatch) string {
	if m.Prefix != "" {
		p := m.Prefix
		if p[len(p)-1] != '/' {
			p += "/"
		}
		p += "**"
		return utils.GetTrieKey(method, p) // 你现有的 key 规范化工具
	}
	return utils.GetTrieKey(method, m.Path)
}
