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