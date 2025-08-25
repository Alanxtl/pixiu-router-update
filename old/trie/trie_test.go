package trie

// =================================================================
// 测试1: 并发安全测试 (此测试预期会引发 panic)
// 运行方式: go test -v -run TestConcurrency
// 警告: 此测试会因为并发读写 map 导致程序崩溃，这是预期行为，证明了代码的非线程安全性。
// =================================================================
//func TestConcurrency(t *testing.T) {
//	t.Log("开始并发测试... 预期会触发 panic: 'fatal error: concurrent map read and map write' 或 'concurrent map writes'")
//	t.Log("如果程序崩溃，则证明了该Trie实现不是线程安全的。")
//
//	trie := NewTrie()
//	var wg sync.WaitGroup
//	numRoutines := 100 // 模拟100个并发请求
//
//	defer func() {
//		if r := recover(); r != nil {
//			wg.Done()
//			t.Log("并发测试完成 (出现竟态)")
//		}
//	}()
//
//	// 启动写goroutines
//	for i := 0; i < numRoutines/2; i++ {
//		wg.Add(1)
//		go func(i int) {
//			defer wg.Done()
//			path := fmt.Sprintf("/api/v1/user/%d", i)
//			_, _ = trie.Put(path, "some data")
//		}(i)
//	}
//
//	// 启动读goroutines
//	for i := 0; i < numRoutines/2; i++ {
//		wg.Add(1)
//		go func(i int) {
//			defer wg.Done()
//			path := fmt.Sprintf("/api/v1/user/%d", i)
//			_, _, _ = trie.Match(path)
//		}(i)
//	}
//
//	wg.Wait()
//	t.Log("并发测试完成 (如果没有崩溃，说明并发冲突未被触发，但风险依旧存在)")
//}
