// bench_test.go
package pixiu_router_update

import (
	"math/rand"
	"net/http"
	"strconv"
	"testing"

	newrouter "github.com/alanxtl/pixiu-router-update/new"
	newmodel "github.com/alanxtl/pixiu-router-update/new/model"

	oldrouter "github.com/alanxtl/pixiu-router-update/old"
	oldmodel "github.com/alanxtl/pixiu-router-update/old/model"
)

// ============= 测试形状与数据生成 =============

type benchShape struct {
	NRoutes         int     // 路由条数
	PrefixRatio     float64 // 前缀路由占比（其余为精确 path）
	HeaderOnlyRatio float64 // 仅 header 路由占比（无 path/prefix）
	Methods         []string
}

// -------- old 模型的一组构造器 --------

func genRoutesOld(sh benchShape) []*oldmodel.Router {
	routes := make([]*oldmodel.Router, 0, sh.NRoutes)
	if len(sh.Methods) == 0 {
		sh.Methods = []string{"GET", "POST"}
	}
	nHeader := int(float64(sh.NRoutes) * sh.HeaderOnlyRatio)
	nPrefix := int(float64(sh.NRoutes-nHeader) * sh.PrefixRatio)
	nPath := sh.NRoutes - nHeader - nPrefix

	// 1) Header-only
	for i := 0; i < nHeader; i++ {
		id := "hdr-" + strconv.Itoa(i)
		r := &oldmodel.Router{
			ID: id,
			Match: oldmodel.RouterMatch{
				Methods: sh.Methods,
				Headers: []oldmodel.HeaderMatcher{
					{Name: "X-Env", Values: []string{"prod"}, Regex: false},
				},
			},
			Route: oldmodel.RouteAction{Cluster: "c-h-" + id},
		}
		routes = append(routes, r)
	}
	// 2) Prefix routes
	for i := 0; i < nPrefix; i++ {
		id := "pre-" + strconv.Itoa(i)
		p := "/api/v1/service" + strconv.Itoa(i%50) + "/"
		r := &oldmodel.Router{
			ID: id,
			Match: oldmodel.RouterMatch{
				Methods: sh.Methods,
				Prefix:  p,
			},
			Route: oldmodel.RouteAction{Cluster: "c-p-" + id},
		}
		routes = append(routes, r)
	}
	// 3) Exact path
	for i := 0; i < nPath; i++ {
		id := "pth-" + strconv.Itoa(i)
		pp := "/api/v1/item/" + strconv.Itoa(i)
		r := &oldmodel.Router{
			ID: id,
			Match: oldmodel.RouterMatch{
				Methods: sh.Methods,
				Path:    pp,
			},
			Route: oldmodel.RouteAction{Cluster: "c-x-" + id},
		}
		routes = append(routes, r)
	}
	return routes
}

func buildOldCoordinator(routes []*oldmodel.Router) *oldrouter.RouterCoordinator {
	cfg := &oldmodel.RouteConfiguration{
		Routes:  routes,
		Dynamic: false,
	}
	return oldrouter.CreateRouterCoordinator(cfg)
}

func buildDeltaOld(base []*oldmodel.Router, seed int64) []*oldmodel.Router {
	cp := make([]*oldmodel.Router, len(base))
	copy(cp, base)
	rnd := rand.New(rand.NewSource(seed))
	k := len(cp) / 100 // 1%
	out := make([]*oldmodel.Router, 0, k)
	for i := 0; i < k; i++ {
		idx := rnd.Intn(len(cp))
		old := cp[idx]
		newPath := "/api/v1/item/" + strconv.Itoa(rnd.Intn(100000))
		nr := &oldmodel.Router{
			ID: old.ID, // 保持 ID，不同 Match，等价“更新”
			Match: oldmodel.RouterMatch{
				Methods: old.Match.Methods,
				Path:    newPath,
				Headers: old.Match.Headers,
			},
			Route: old.Route,
		}
		out = append(out, nr)
	}
	return out
}

// -------- new 模型的一组构造器 --------

func genRoutesNew(sh benchShape) []*newmodel.Router {
	routes := make([]*newmodel.Router, 0, sh.NRoutes)
	if len(sh.Methods) == 0 {
		sh.Methods = []string{"GET", "POST"}
	}
	nHeader := int(float64(sh.NRoutes) * sh.HeaderOnlyRatio)
	nPrefix := int(float64(sh.NRoutes-nHeader) * sh.PrefixRatio)
	nPath := sh.NRoutes - nHeader - nPrefix

	// 1) Header-only
	for i := 0; i < nHeader; i++ {
		id := "hdr-" + strconv.Itoa(i)
		r := &newmodel.Router{
			ID: id,
			Match: newmodel.RouterMatch{
				Methods: sh.Methods,
				Headers: []newmodel.HeaderMatcher{
					{Name: "X-Env", Values: []string{"prod"}, Regex: false},
				},
			},
			Route: newmodel.RouteAction{Cluster: "c-h-" + id},
		}
		routes = append(routes, r)
	}
	// 2) Prefix routes
	for i := 0; i < nPrefix; i++ {
		id := "pre-" + strconv.Itoa(i)
		p := "/api/v1/service" + strconv.Itoa(i%50) + "/"
		r := &newmodel.Router{
			ID: id,
			Match: newmodel.RouterMatch{
				Methods: sh.Methods,
				Prefix:  p,
			},
			Route: newmodel.RouteAction{Cluster: "c-p-" + id},
		}
		routes = append(routes, r)
	}
	// 3) Exact path
	for i := 0; i < nPath; i++ {
		id := "pth-" + strconv.Itoa(i)
		pp := "/api/v1/item/" + strconv.Itoa(i)
		r := &newmodel.Router{
			ID: id,
			Match: newmodel.RouterMatch{
				Methods: sh.Methods,
				Path:    pp,
			},
			Route: newmodel.RouteAction{Cluster: "c-x-" + id},
		}
		routes = append(routes, r)
	}
	return routes
}

func buildNewCoordinator(routes []*newmodel.Router) *newrouter.RouterCoordinator {
	cfg := &newmodel.RouteConfiguration{
		Routes:  routes,
		Dynamic: false,
	}
	return newrouter.CreateRouterCoordinator(cfg)
}

func buildDeltaNew(base []*newmodel.Router, seed int64) []*newmodel.Router {
	cp := make([]*newmodel.Router, len(base))
	copy(cp, base)
	rnd := rand.New(rand.NewSource(seed))
	k := len(cp) / 100 // 1%
	out := make([]*newmodel.Router, 0, k)
	for i := 0; i < k; i++ {
		idx := rnd.Intn(len(cp))
		old := cp[idx]
		newPath := "/api/v1/item/" + strconv.Itoa(rnd.Intn(100000))
		nr := &newmodel.Router{
			ID: old.ID,
			Match: newmodel.RouterMatch{
				Methods: old.Match.Methods,
				Path:    newPath,
				Headers: old.Match.Headers,
			},
			Route: old.Route,
		}
		out = append(out, nr)
	}
	return out
}

// ============= 请求集（两边共享 http.Request） =============

func genRequests(n int) []*http.Request {
	reqs := make([]*http.Request, 0, n)
	methods := []string{"GET", "POST"}
	for i := 0; i < n; i++ {
		var path string
		switch i % 3 {
		case 0:
			path = "/api/v1/item/" + strconv.Itoa(i%10000)
		case 1:
			path = "/api/v1/service" + strconv.Itoa(i%50) + "/foo/bar"
		default:
			path = "/unknown/" + strconv.Itoa(i)
		}
		req, _ := http.NewRequest(methods[i%len(methods)], path, nil)
		if i%5 == 0 { // 触发 header-only 路由
			req.Header.Set("X-Env", "prod")
		}
		reqs = append(reqs, req)
	}
	return reqs
}

// ============= Bench 1：读吞吐（单线程） =============

func BenchmarkRoute_ReadThroughput(b *testing.B) {
	shape := benchShape{NRoutes: 30000, PrefixRatio: 0.4, HeaderOnlyRatio: 0.1, Methods: []string{"GET", "POST"}}

	oldRoutes := genRoutesOld(shape)
	newRoutes := genRoutesNew(shape)
	reqs := genRequests(4096)

	oldc := buildOldCoordinator(oldRoutes)
	newc := buildNewCoordinator(newRoutes)

	b.Run("old/locked-read", func(b *testing.B) {
		rand.Seed(1)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			r := reqs[i%len(reqs)]
			_, _ = oldc.Route(r)
		}
	})

	b.Run("new/rcu-read", func(b *testing.B) {
		rand.Seed(1)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			r := reqs[i%len(reqs)]
			_, _ = newc.Route(r)
		}
	})
}

// ============= Bench 2：读吞吐（并行） =============

func BenchmarkRoute_ReadParallel(b *testing.B) {
	shape := benchShape{NRoutes: 30000, PrefixRatio: 0.4, HeaderOnlyRatio: 0.1, Methods: []string{"GET", "POST"}}

	oldRoutes := genRoutesOld(shape)
	newRoutes := genRoutesNew(shape)
	reqs := genRequests(8192)

	oldc := buildOldCoordinator(oldRoutes)
	newc := buildNewCoordinator(newRoutes)

	b.Run("old/parallel", func(b *testing.B) {
		rand.Seed(2)
		b.ReportAllocs()
		b.SetParallelism(4)
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			i := rand.Int()
			for pb.Next() {
				r := reqs[i%len(reqs)]
				_, _ = oldc.Route(r)
				i++
			}
		})
	})

	b.Run("new/parallel", func(b *testing.B) {
		rand.Seed(2)
		b.ReportAllocs()
		b.SetParallelism(4)
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			i := rand.Int()
			for pb.Next() {
				r := reqs[i%len(reqs)]
				_, _ = newc.Route(r)
				i++
			}
		})
	})
}

// ============= Bench 3：频繁变更生效（1% 路由每次重载） =============

func BenchmarkReload_Latency(b *testing.B) {
	shape := benchShape{NRoutes: 30000, PrefixRatio: 0.4, HeaderOnlyRatio: 0.1, Methods: []string{"GET", "POST"}}
	oldBase := genRoutesOld(shape)
	newBase := genRoutesNew(shape)

	oldc := buildOldCoordinator(oldBase)
	newc := buildNewCoordinator(newBase)

	b.Run("old/reload-1percent", func(b *testing.B) {
		rand.Seed(3)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for _, r := range buildDeltaOld(oldBase, int64(i)) {
				oldc.OnAddRouter(r)
			}
		}
	})

	b.Run("new/reload-1percent", func(b *testing.B) {
		rand.Seed(3)
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for _, r := range buildDeltaNew(newBase, int64(i)) {
				newc.OnAddRouter(r)
			}
		}
	})
}

// ============= Bench 4：RouteByPathAndName（API 行为一致性） =============

func BenchmarkRouteByPathAndName(b *testing.B) {
	shape := benchShape{
		NRoutes:         20000,
		PrefixRatio:     0.5,
		HeaderOnlyRatio: 0.0,
		Methods:         []string{"GET"},
	}

	oldc := buildOldCoordinator(genRoutesOld(shape))
	newc := buildNewCoordinator(genRoutesNew(shape))

	paths := []string{
		"/api/v1/item/12345",
		"/api/v1/service7/xxx/yyy",
		"/no/match/path",
	}
	method := "GET"

	b.Run("old/RouteByPathAndName", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			path := paths[i%len(paths)]
			_, _ = oldc.RouteByPathAndName(path, method)
		}
	})

	b.Run("new/RouteByPathAndName", func(b *testing.B) {
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			path := paths[i%len(paths)]
			_, _ = newc.RouteByPathAndName(path, method)
		}
	})
}
