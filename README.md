# pixiu-router-update

## old trie implementation benchmark result

注意：由于Trie原始数据结构不是线程安全的，BenchmarkMatchParallel测试仅在“无写入”的理想情况下才能成功运行。

```
goos: linux
goarch: amd64
pkg: github.com/alanxtl/pixiu-router-update
cpu: Intel(R) Core(TM) Ultra 5 125H
BenchmarkRouterPerformance/PathOnly-100-Routes-18         	 8599695	       149.6 ns/op	     160 B/op	       5 allocs/op
BenchmarkRouterPerformance/PathOnly-1000-Routes-18        	 5471448	       221.8 ns/op	     160 B/op	       5 allocs/op
BenchmarkRouterPerformance/PathOnly-10000-Routes-18       	  990483	      1230 ns/op	     160 B/op	       5 allocs/op
BenchmarkRouterPerformance/HeaderOnly-100-Routes-18       	 2761293	       411.1 ns/op	       8 B/op	       1 allocs/op
BenchmarkRouterPerformance/HeaderOnly-1000-Routes-18      	  284982	      4154 ns/op	       8 B/op	       1 allocs/op
BenchmarkRouterPerformance/HeaderOnly-10000-Routes-18     	   31845	     37987 ns/op	       8 B/op	       1 allocs/op
BenchmarkRouterWithContention/RouterWithContention-100-Routes-18         	 8034261	       150.0 ns/op	     160 B/op	       5 allocs/op
BenchmarkRouterWithContention/RouterWithContention-1000-Routes-18        	 5257695	       229.8 ns/op	     160 B/op	       5 allocs/op
BenchmarkRouterWithContention/RouterWithContention-10000-Routes-18       	  918974	      1327 ns/op	     160 B/op	       5 allocs/op
BenchmarkTrieMatch/Routes-100-18                                         	 6678661	       161.2 ns/op	      96 B/op	       2 allocs/op
BenchmarkTrieMatch/Routes-1000-18                                        	 5993542	       199.5 ns/op	      96 B/op	       2 allocs/op
BenchmarkTrieMatch/Routes-10000-18                                       	 5268208	       231.9 ns/op	      96 B/op	       2 allocs/op
BenchmarkTrieMatch/Routes-100000-18                                      	 4065294	       307.4 ns/op	      96 B/op	       2 allocs/op
BenchmarkTrieMatchParallel/Routes-100-18                                 	18814371	        60.73 ns/op	      96 B/op	       2 allocs/op
BenchmarkTrieMatchParallel/Routes-1000-18                                	16890303	        72.34 ns/op	      96 B/op	       2 allocs/op
BenchmarkTrieMatchParallel/Routes-10000-18                               	17749135	        62.41 ns/op	      96 B/op	       2 allocs/op
BenchmarkTrieMatchParallel/Routes-100000-18                              	16548598	        66.48 ns/op	      96 B/op	       2 allocs/op
BenchmarkTriePut/Add-New-Routes-18                                       	 1000000	      1135 ns/op	     691 B/op	       9 allocs/op
PASS
ok  	github.com/alanxtl/pixiu-router-update	26.811s
```

底层的**Trie数据结构极其快速且可扩展**，而**`RouterCoordinator`实现层引入了严重的性能瓶颈**，在规模化时抵消了Trie的优势。

***

### **执行摘要** 📜

* **Trie核心表现优异**：独立的Trie（`BenchmarkTrieMatch`）表现出完美的可扩展性，随着路由数量增加1000倍，性能仅轻微下降。
* **`RouterCoordinator`瓶颈**：`RouterCoordinator`的路径路由（`PathOnly`）存在严重的实现问题，导致性能在规模化时剧烈且意外地下降。
* **头部路由是性能陷阱**：基于HTTP头的路由（`HeaderOnly`）被确认是线性扫描操作（O(N)），随着规则数量的增加，导致性能急剧下降。
* **`RWMutex`竞争严重**：虽然写操作对读取延迟影响较小，但反向影响是灾难性的。高读取流量可能将单次配置更新的延迟增加至**80倍**，对操作灵活性构成重大风险。

***

### **详细性能分析**

#### **好的方面：底层Trie性能（基准）**

`BenchmarkTrieMatch`结果为路由性能设定了明确的基准。

| Trie路由数量 | 单核平均延迟 | 多核平均延迟 | 可扩展性分析 |
| :--- | :--- | :--- | :--- |
| 100 | 168.0 ns | 64.92 ns | （基准） |
| 1,000 | 208.0 ns | 77.97 ns | 优秀 |
| 10,000 | 235.5 ns | 61.92 ns | 优秀 |
| 100,000 | 294.3 ns | 63.71 ns | **优秀** |

* **卓越可扩展性**：当路由数量增加1000倍（从100到100,000）时，单核延迟仅增加**75%**（从168ns到294ns）。这证明了Trie的性能与规则总数无关，而是与路径的深度有关，这正是所期望的行为。
* **高吞吐量**：在100,000条路由下，Trie可以在单核上执行约340万次匹配，每秒在并行时超过**1500万次匹配**。

***

#### **不好的方面：`RouterCoordinator`性能下降**

通过`RouterCoordinator`包装使用Trie时，性能故事发生了剧烈变化。

| 路由数量 | 底层Trie延迟（ns/op） | `RouterCoordinator`延迟（ns/op） | **包装开销** |
| :--- | :--- | :--- | :--- |
| 100 | 168.0 ns | 141.1 ns | -16%（可忽略） |
| 1,000 | 208.0 ns | 212.7 ns | +2%（可接受） |
| **10,000** | **235.5 ns** | **1180 ns** | **+400%（严重下降）** |

* **问题**：数据清楚地显示`RouterCoordinator`层在10,000条路由时引入了巨大的**400%性能开销**。包装的实现存在扩展性问题，抵消了使用Trie的主要好处。
* **结论**：性能瓶颈**不**在于核心Trie数据结构，而是在使用它的业务逻辑层。

***

#### **丑陋的方面：头部路由和锁竞争**

##### **基于头部的路由**

| 头部路由数量 | 平均延迟（ns/op） | 与100条路由的性能比较 |
| :--- | :--- | :--- |
| 100 | 426.7 ns | （基准） |
| 1,000 | 4062 ns | **慢9.5倍** |
| 10,000 | 40061 ns | **慢94倍** |

* **线性扩展确认**：随着规则数量增加，性能线性下降。在10,000条路由下，单次匹配耗时超过**40微秒**，这对于高性能网关而言是不可接受的。应避免在大规则集上使用这种路由方法。

##### **读/写锁竞争**

此分析揭示了使用标准`RWMutex`的双向成本。

1. **写操作对读的影响**：
    * **基准（10k路由，无写操作）**：1180 ns/op
    * **有竞争（10k路由，有写操作）**：1309 ns/op
    * **影响**：配置更新导致平均读取延迟**增加11%**。

2. **读操作对写的影响（关键问题）**：
    * **基准（10k路由，无读操作）**：2721 ns/op（约2.7微秒）
    * **有竞争（10k路由，有读操作）**：218709 ns/op（约218.7微秒）
    * **影响**：高读取流量使写操作**变慢80倍**。一个应在约3微秒内完成的配置更新现在需要超过200微秒，因为它必须等待所有并发读取操作完成。

***