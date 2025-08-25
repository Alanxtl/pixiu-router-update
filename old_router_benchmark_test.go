package pixiu_router_update

import (
	"context"
	"fmt"
	stdHttp "net/http"
	"runtime"
	"sync"
	"testing"
	"time"
)

import (
	"github.com/alanxtl/pixiu-router-update/old"
	"github.com/alanxtl/pixiu-router-update/old/model"
)

// createTestRequest 是一个辅助函数，用于创建测试用的HTTP请求。
func createTestRequest(method, path string, headers map[string]string) *stdHttp.Request {
	req, _ := stdHttp.NewRequest(method, "http://localhost"+path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return req
}

// setupCoordinator 是一个辅助函数，用于构建包含指定数量路由的RouterCoordinator。
func setupCoordinator(b *testing.B, numPathRoutes, numHeaderRoutes int) (*old.RouterCoordinator, []*stdHttp.Request) {
	b.Helper()

	var routes []*model.Router
	var requests []*stdHttp.Request

	// 1. 创建基于路径的路由 (利用Trie)
	for i := 0; i < numPathRoutes; i++ {
		path := fmt.Sprintf("/api/v1/path/%d", i)
		routes = append(routes, &model.Router{
			Match: model.RouterMatch{
				Prefix:  path,
				Methods: []string{"GET"},
			},
			Route: model.RouteAction{Cluster: fmt.Sprintf("cluster_%d", i)},
		})
		requests = append(requests, createTestRequest("GET", path, nil))
	}

	// 2. 创建基于头部的路由 (线性扫描)
	for i := 0; i < numHeaderRoutes; i++ {
		headerName := fmt.Sprintf("X-Route-Id")
		headerValue := fmt.Sprintf("header-%d", i)
		routes = append(routes, &model.Router{
			Match: model.RouterMatch{
				// 关键：不设置Prefix或Path，强制进入Header匹配逻辑
				Headers: []model.HeaderMatcher{{Name: headerName, Values: []string{headerValue}}},
				Methods: []string{"POST"},
			},
			Route: model.RouteAction{Cluster: fmt.Sprintf("cluster_header_%d", i)},
		})
		requests = append(requests, createTestRequest("POST", "/any/path", map[string]string{headerName: headerValue}))
	}

	config := &model.RouteConfiguration{Routes: routes}
	coordinator := old.CreateRouterCoordinator(config)

	b.ResetTimer()
	return coordinator, requests
}

// BenchmarkRouterPerformance 测试不同路由策略下的性能
func BenchmarkRouterPerformance(b *testing.B) {
	scenarios := []struct {
		name            string
		numPathRoutes   int
		numHeaderRoutes int
	}{
		{"PathOnly-100-Routes", 100, 0},
		{"PathOnly-1000-Routes", 1000, 0},
		{"PathOnly-10000-Routes", 10000, 0},
		{"HeaderOnly-100-Routes", 0, 100},
		{"HeaderOnly-1000-Routes", 0, 1000},
		{"HeaderOnly-10000-Routes", 0, 10000},
	}

	for _, s := range scenarios {
		b.Run(s.name, func(b *testing.B) {
			coordinator, requests := setupCoordinator(b, s.numPathRoutes, s.numHeaderRoutes)
			if len(requests) == 0 {
				b.Skip("无请求可供测试")
			}
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					req := requests[i%len(requests)]
					_, _ = coordinator.Route(req)
					i++
				}
			})
		})
	}
}

// BenchmarkRouterWithContention 模拟读写竞争下的性能
func BenchmarkRouterWithContention(b *testing.B) {
	//b.Log("测试在持续读取时，偶尔写入造成的性能影响")
	scenarios := []struct {
		name            string
		numPathRoutes   int
		numHeaderRoutes int
	}{
		{"RouterWithContention-100-Routes", 100, 0},
		{"RouterWithContention-1000-Routes", 1000, 0},
		{"RouterWithContention-10000-Routes", 10000, 0},
	}

	for _, s := range scenarios {
		b.Run(s.name, func(b *testing.B) {
			coordinator, requests := setupCoordinator(b, s.numPathRoutes, s.numHeaderRoutes)

			// 后台goroutine，模拟每10毫秒一次的配置更新
			stopCh := make(chan struct{})
			go func() {
				ticker := time.NewTicker(10 * time.Millisecond)
				defer ticker.Stop()
				i := 0
				for {
					select {
					case <-ticker.C:
						// 添加一个新路由，然后删除它
						router := &model.Router{
							Match: model.RouterMatch{Prefix: fmt.Sprintf("/new/%d", i), Methods: []string{"GET"}},
							Route: model.RouteAction{Cluster: "new_cluster"},
						}
						coordinator.OnAddRouter(router)
						coordinator.OnDeleteRouter(router)
						i++
					case <-stopCh:
						return
					}
				}
			}()

			b.ResetTimer()

			// 并发执行路由匹配
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					req := requests[i%len(requests)]
					_, _ = coordinator.Route(req)
					i++
				}
			})

			b.StopTimer()
			close(stopCh)
		})
	}
}

// BenchmarkWriteLatencyWithoutReaders 测试在无并发读取的情况下，单次写入操作的延迟。
func BenchmarkWriteLatencyWithoutReaders(b *testing.B) {
	scenarios := []struct {
		name            string
		numPathRoutes   int
		numHeaderRoutes int
	}{
		{"No-Readers-Contention-100-Routes", 100, 0},
		{"No-Readers-Contention-1000-Routes", 1000, 0},
		{"No-Readers-Contention-10000-Routes", 10000, 0},
	}

	for _, s := range scenarios {
		b.Run(s.name, func(b *testing.B) {
			coordinator, _ := setupCoordinator(b, s.numPathRoutes, s.numHeaderRoutes)

			b.ResetTimer() // 关键：只测量写入操作

			for i := 0; i < b.N; i++ {
				b.StopTimer() // 暂停计时器，准备要添加的数据
				router := &model.Router{
					Match: model.RouterMatch{Prefix: fmt.Sprintf("/new/%d", i), Methods: []string{"GET"}},
					Route: model.RouteAction{Cluster: "new_cluster"},
				}
				b.StartTimer() // 恢复计时器，开始测量

				coordinator.OnAddRouter(router)
			}
		})
	}
}

// BenchmarkWriteLatencyWithReaders 测试在大量并发读取的压力下，单次写入操作的延迟。
func BenchmarkWriteLatencyWithReaders(b *testing.B) {
	scenarios := []struct {
		name            string
		numPathRoutes   int
		numHeaderRoutes int
	}{
		{"With-Readers-Contention-100-Routes", 100, 0},
		{"With-Readers-Contention-1000-Routes", 1000, 0},
		{"With-Readers-Contention-10000-Routes", 10000, 0},
	}

	for _, s := range scenarios {
		b.Run(s.name, func(b *testing.B) {
			coordinator, requests := setupCoordinator(b, s.numPathRoutes, s.numHeaderRoutes)

			// 创建一个上下文，用于在测试结束后通知所有reader goroutine停止
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			var wg sync.WaitGroup
			// 启动GOMAXPROCS * 4个goroutine在后台持续发送读请求
			numReaders := runtime.GOMAXPROCS(0) * 4
			wg.Add(numReaders)

			for i := 0; i < numReaders; i++ {
				go func() {
					defer wg.Done()
					j := 0
					for {
						select {
						case <-ctx.Done(): // 如果上下文被取消，则退出循环
							return
						default:
							// 持续执行读操作
							req := requests[j%len(requests)]
							_, _ = coordinator.Route(req)
							j++
						}
					}
				}()
			}

			b.ResetTimer() // 关键：只测量写入操作

			// 主循环，测量b.N次写操作的性能
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				router := &model.Router{
					Match: model.RouterMatch{Prefix: fmt.Sprintf("/new/%d", i), Methods: []string{"GET"}},
					Route: model.RouteAction{Cluster: "new_cluster"},
				}
				b.StartTimer()

				// 这是我们要测量的核心操作
				coordinator.OnAddRouter(router)
			}

			// 测试结束后，停止所有reader并等待它们退出
			b.StopTimer()
			cancel()
			wg.Wait()
		})
	}
}
