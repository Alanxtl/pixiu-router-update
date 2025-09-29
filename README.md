# pixiu-router-update

## new/old trie implementation benchmark result

```
go test -bench . -benchmem
goos: linux
goarch: amd64
pkg: github.com/alanxtl/pixiu-router-update
cpu: Intel(R) Core(TM) Ultra 5 125H
BenchmarkRoute_ReadThroughput/old/locked-read-30k-18         	    9500	    125474 ns/op	   14811 B/op	       8 allocs/op
BenchmarkRoute_ReadThroughput/new/rcu-read-30k-18            	   35670	     33505 ns/op	     202 B/op	       4 allocs/op
BenchmarkRoute_ReadParallel/old/parallel-30k-18              	   47488	     22225 ns/op	   14931 B/op	       8 allocs/op
BenchmarkRoute_ReadParallel/new/parallel-30k-18              	  244779	      6010 ns/op	     202 B/op	       5 allocs/op
BenchmarkReload_Latency/old/reload-1percent-30k-18           	    1508	    730331 ns/op	  636910 B/op	    6387 allocs/op
BenchmarkReload_Latency/new/reload-1percent-30k-18           	    4962	    237589 ns/op	  301039 B/op	     902 allocs/op
BenchmarkRoute_100k_ReadThroughput/old/locked-read-100k-18   	    1501	    838616 ns/op	   62477 B/op	       9 allocs/op
BenchmarkRoute_100k_ReadThroughput/new/rcu-read-100k-18      	   10000	    108635 ns/op	     202 B/op	       5 allocs/op
BenchmarkRoute_100k_ReadParallel/old/parallel-100k-18        	   13701	     82713 ns/op	   63254 B/op	       9 allocs/op
BenchmarkRoute_100k_ReadParallel/new/parallel-100k-18        	   76260	     16033 ns/op	     201 B/op	       5 allocs/op
BenchmarkReload_100k_Latency_1Percent/old/reload-1percent-100k-18         	     536	   2175171 ns/op	 2065890 B/op	   21069 allocs/op
BenchmarkReload_100k_Latency_1Percent/new/reload-1percent-100k-18         	    1171	    878919 ns/op	  973764 B/op	    3001 allocs/op
BenchmarkRouteByPathAndName/old/RouteByPathAndName-18                     	 1320177	       827.5 ns/op	     429 B/op	       9 allocs/op
BenchmarkRouteByPathAndName/new/RouteByPathAndName-18                     	 4387951	       271.2 ns/op	     157 B/op	       5 allocs/op
PASS
ok  	github.com/alanxtl/pixiu-router-update	25.681s
```

benchmark 运行方法

```
go test -bench . -benchmem
```

### Pixiu 路由引擎升级实践：从全局锁到 RCU 快照

#### 1. 背景：全局锁带来的性能瓶颈

Pixiu 网关的路由模块早期实现中，底层使用 Trie 树来匹配路由规则。
为保证并发安全，所有 **读操作（请求匹配）** 和 **写操作（规则更新）** 都由一把全局互斥锁（`sync.Mutex`）来保护。

这种设计的优点是实现简单，能有效避免并发冲突。但缺点也同样明显：

* **读写阻塞**：写操作会持有锁，导致所有进行中的读请求被阻塞。
* **性能瓶颈**：在高并发场景下，即使是少量的写操作，也会严重影响整体吞吐量，导致请求延迟上升。

**旧实现伪代码：**

```go
type Router struct {
    mu   sync.Mutex
    trie *Trie
}

func (r *Router) Match(req *http.Request) *Route {
    r.mu.Lock()
    defer r.mu.Unlock()
    return r.trie.Match(req.URL.Path)
}

func (r *Router) Reload(cfg *Config) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.trie = BuildTrie(cfg)
}
```

#### 2. 技术选型：无锁方案对比

为了解决全局锁的瓶颈，我们的核心目标是实现**读取无锁**。我们评估了两种方案：

1.  **双树 + 命令日志**: 维护一个“读树”和一个“写树”。写操作记录在命令日志中，由后台任务应用到“写树”。定期切换两棵树的角色。此方案读性能好，但实现复杂，且有数据同步延迟。
2.  **RCU (Read-Copy-Update) / 快照**: 读取时直接访问当前的路由快照。写入时，在后台完整构建一个新的Trie树快照，然后通过原子操作（`atomic.Pointer`）替换掉旧的快照。

**方案对比：**

| 维度 | A. 双树 + 命令队列 | B. RCU/原子快照 |
| :--- | :--- | :--- |
| **读性能** | 无锁，极快 | 无锁，极快 |
| **写延迟** | 取决于切换周期，准实时 | 取决于全量构建时间 |
| **内存占用** | 长期维持两份数据 | 仅在更新瞬间产生两份数据 |
| **实现复杂度** | 高（需维护队列、切换逻辑） | 低（构建+原子替换） |
| **可靠性** | 故障点多，恢复复杂 | 故障面小，无中间状态 |

考虑到实现复杂度和可靠性，双树需要考虑命令日志的定义/幂等/压缩/落盘、双游标一致性、恢复流程、观测与回放工具链。，我们选择 **方案 B (RCU/快照)**，它能以较低的成本解决核心的读写冲突问题。

#### 3. RCU 快照方案实现

新架构的核心是使用 `atomic.Value` 来存储指向当前路由快照的指针。

* **读操作**：通过 `atomic.Load()` 无锁获取当前快照的指针，然后访问该快照进行路由匹配。
* **写操作**：在后台根据新配置构建一个全新的、不可变的快照。构建完成后，通过 `atomic.Store()` 将 `atomic.Value` 中的指针替换为新快照的指针。旧快照在没有被任何请求引用后，将由 Go 的 GC 自动回收。

**核心代码：**

```go
type RouterCoordinator struct {
    active atomic.Value // 存储 *routeSnapshot
}

type routeSnapshot struct {
    methodTries map[string]*Trie
    // ... 其他不可变的路由规则
}

// 读操作：无锁
func (rc *RouterCoordinator) Route(req *http.Request) (*RouteAction, error) {
    s := rc.active.Load().(*routeSnapshot)
    // ... 使用 s 进行匹配 ...
    return t.Match(req.URL.Path)
}

// 写操作：构建并原子替换
func (rc *RouterCoordinator) Reload(cfg *Config) {
    snapshot := buildSnapshot(cfg)
    rc.active.Store(snapshot)
}
```

**架构对比图：**

* **旧架构：全局锁**

  ```mermaid
  sequenceDiagram
      participant Client1 as Client A (Read)
      participant Client2 as Client B (Read)
      participant Admin as Admin (Write)
      participant Router as Router (Global Lock)
      participant Trie as Trie

      Client1->>Router: Route(req1)
      activate Router
      Router->>Trie: Match(path1)
      Trie-->>Router: RouteAction
      Router-->>Client1: Result (OK)
      deactivate Router

      Admin->>Router: Reload(new config)
      activate Router
      Note over Router: 持锁重建
      Router->>Trie: BuildTrie(new cfg)
      Trie-->>Router: New Trie
      deactivate Router
      Router-->>Admin: Done

      Client2->>Router: Route(req2)
      activate Router
      Router->>Trie: Match(path2)
      Trie-->>Router: RouteAction
      Router-->>Client2: Result (OK)
      deactivate Router
  ```

* **新架构：RCU 快照**

  ```mermaid
  sequenceDiagram
      participant Client1 as Client A (Read)
      participant Client2 as Client B (Read)
      participant Admin as Admin (Write)
      participant Router as Router (RCU)
      participant SnapA as Snapshot A (Active)
      participant SnapB as Snapshot B (Building)

      Client1->>Router: Route(req1)
      Router->>SnapA: Match(path1)
      SnapA-->>Router: RouteAction
      Router-->>Client1: Result (OK)

      Admin->>Router: Reload(new config)
      Note over Router,SnapB: 后台构建 Snapshot B
      Router->>SnapB: Build from new cfg
      SnapB-->>Router: Built

      Note over Router: Atomic Switch
      Router->>Router: active = SnapB

      Client2->>Router: Route(req2)
      Router->>SnapB: Match(path2)
      SnapB-->>Router: RouteAction
      Router-->>Client2: Result (OK)
  ```

#### 4. 爆表时刻：树重建延迟

新架构在读性能上表现优异，但在对10万规模的路由进行更新时，写性能（Reload）出现了严重问题。

**基准测试暴露了瓶颈：**

```
# 写性能（Reload）压测
BenchmarkReload_Latency/old/reload-1percent-18   ...  1030785 ns/op    // 旧方案：约 1ms
BenchmarkReload_Latency/new/reload-1percent-18   ...  32922520653 ns/op  // 新方案：约 32 秒！
```

全量构建一个大规模Trie树快照的开销巨大，导致更新操作耗时从毫秒级飙升至数十秒，这在生产上是不可接受的。

#### 5. 第一轮优化：调整构建逻辑

为了解决构建延迟问题，我们首先从宏观的构建逻辑和CPU热点入手，进行了第一轮优化：

1.  **延迟与分批构建**：将一次性的大量内存分配操作打散，避免瞬间给GC带来过大压力。
2.  **缓存正则表达式**：路由规则中的 `Header` 匹配存在大量正则表达式。`regexp.Compile` 是一个高消耗操作，我们为它增加了全局缓存，避免对相同表达式的重复编译。

**第一轮优化效果：**

经过这轮优化，在10万路由规模下，Reload 延迟已经从 **“数十秒”** 降低到了 **“百毫秒级”**，基本满足了大部分场景的需求。

```
# 10万路由，1%更新
BenchmarkReload_100k_Latency_1Percent/new/reload-1percent-100k-18 ... 2394690 ns/op # 约 2.4ms
```

#### 6. 继续上探：压榨内存性能

虽然构建延迟已大幅降低，但当我们将路由规模上探到**百万级别**时，构建过程中的**内存分配和GC压力**成为了新的瓶颈。

为此，我们进行了更深入的第二轮、面向内存的优化：

1.  **构建期对象池化**：我们引入 `sync.Pool` 来复用构建过程中需要频繁创建的临时切片（如 `compiledHeader[]`）。从池中获取，使用完毕后清空并归还，极大地减少了临时对象的分配次数。

    **代码示例 (池化片段):**

    ```go
    var compiledHeaderSlicePool = sync.Pool{
        New: func() any {
            s := make([]compiledHeader, 0, 4)
            return &s
        },
    }

    // 使用时
    chPtr := compiledHeaderSlicePool.Get().(*[]compiledHeader)
    // ... 使用 chPtr ...
    compiledHeaderSlicePool.Put(chPtr) // 归还
    ```

2.  **精准预分配**：在构建开始前，先完整扫描一次路由配置，精确计算出各个 `slice` 和 `map` 需要的最终容量，然后一次性 `make` 完成内存分配，从根本上杜绝了 `append` 过程中的多次扩容和数据迁移。

**第二轮优化效果 (百万路由规模):**

这一轮针对内存的优化，取得了显著成效：

* **构建耗时**：下降 **~28%** (从 370ms → 265ms)
* **内存分配总量**：下降 **~30%** (从 115MB → 81MB)
* **对象分配次数**：下降 **~31%** (从 166万次 → 115万次)


#### 7. 最终性能对比

在10万路由规模下，优化后的新旧架构性能对比如下：

| 场景 | 旧架构 (全局锁) | 新架构 (RCU 快照) | 提升 |
| :--- | :--- | :--- | :--- |
| 单线程读 | ~1.1 ms/op | ~0.2 ms/op | **~5x** |
| 并行读 | ~160 µs/op | ~22 µs/op | **~7x** |
| Reload 延迟 | ~3.6 ms | ~1.4 ms | **~2.5x** |

#### 8. 总结

本次升级通过引入RCU快照机制，成功解决了全局锁导致的读写性能瓶颈。

* **成果**：实现了路由读取的完全无锁，读性能提升明显。同时，通过对构建过程的针对性优化，将写操作的延迟控制在合理范围内。
* **经验**：RCU是解决高并发“读多写少”场景锁竞争的有效模式。性能优化是一个发现并解决新瓶颈的迭代过程，需要依赖基准测试和性能剖析工具来指导。

对于更大规模的路由场景，全量构建的开销仍是潜在问题。未来的方向可以探索增量快照更新等方案。