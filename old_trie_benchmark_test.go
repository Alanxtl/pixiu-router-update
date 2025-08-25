package pixiu_router_update

import (
	"fmt"
	"github.com/alanxtl/pixiu-router-update/old/trie"
	"math/rand"
	"testing"
)

// setupTrie 是一个辅助函数，用于创建一个预填充了指定数量路由的Trie实例。
// 它会返回创建好的Trie和用于测试的路由路径列表。
// 在填充完成后，它会重置计时器，确保准备时间不计入测试结果。
func setupTrie(b *testing.B, numRoutes int) (*trie.Trie, []string) {
	b.Helper()

	trie := trie.NewTrie()
	routes := make([]string, numRoutes)

	for i := 0; i < numRoutes; i++ {
		// 创建一些结构化的、有一定深度的路由
		routes[i] = fmt.Sprintf("/api/v1/service-%d/user/%d", i%100, i)
		_, err := trie.Put(routes[i], fmt.Sprintf("biz-info-%d", i))
		if err != nil {
			b.Fatalf("Failed to put route: %v", err)
		}
	}

	// 准备工作完成，重置计时器
	b.ResetTimer()
	return &trie, routes
}

// BenchmarkTrieMatch 测试在不同规模的路由表下，单个CPU核心的路由匹配性能。
func BenchmarkTrieMatch(b *testing.B) {
	// 定义不同的测试场景（路由数量）
	scenarios := []int{
		100,
		1000,
		10000,
		100000,
	}

	for _, numRoutes := range scenarios {
		b.Run(fmt.Sprintf("Routes-%d", numRoutes), func(b *testing.B) {
			trie, routes := setupTrie(b, numRoutes)

			// b.N 是由测试框架决定的迭代次数
			for i := 0; i < b.N; i++ {
				// 随机匹配一个已存在的路由，模拟真实命中场景
				// 使用简单的取模来选择路由，避免随机数生成带来的开销
				trie.Match(routes[i%numRoutes])
			}
		})
	}
}

// BenchmarkTrieMatchParallel 测试在多CPU核心下的并行路由匹配性能。
// 注意：由于此Trie实现不是线程安全的，此测试仅在“无写入”的理想情况下才能成功运行。
// 它展示了在纯读取场景下，无锁读取的理论性能峰值。
func BenchmarkTrieMatchParallel(b *testing.B) {
	scenarios := []int{
		100,
		1000,
		10000,
		100000,
	}

	for _, numRoutes := range scenarios {
		b.Run(fmt.Sprintf("Routes-%d", numRoutes), func(b *testing.B) {
			trie, routes := setupTrie(b, numRoutes)

			// RunParallel 会创建多个goroutine并行执行测试
			b.RunParallel(func(pb *testing.PB) {
				// 每个goroutine有自己的随机数种子，以减少资源竞争
				r := rand.New(rand.NewSource(int64(b.N)))

				for pb.Next() {
					// 随机选择一个路由进行匹配
					trie.Match(routes[r.Intn(numRoutes)])
				}
			})
		})
	}
}

// BenchmarkTriePut 测试连续添加新路由的性能。
func BenchmarkTriePut(b *testing.B) {
	b.Run("Add-New-Routes", func(b *testing.B) {
		trie := trie.NewTrie()
		// 确保计时器在循环外重置
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			// 每次都插入一个全新的路由
			path := fmt.Sprintf("/api/v2/new/service/%d", i)
			trie.Put(path, "new-biz-info")
		}
	})
}
