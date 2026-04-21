package nodes

import "sync/atomic"

var nodes = []string{
	"http://localhost:8001",
	"http://localhost:8002",
}

var counter uint64

func GetNode() string {
	i := atomic.AddUint64(&counter, 1)
	return nodes[int(i)%len(nodes)]
}
