package chinacheck

import (
	"fmt"
	"sync"
)

// flightGroup 极简 singleflight：合并相同 key 的并发调用，只执行一次 fn，
// 其余调用方等待同一结果。对标 golang.org/x/sync/singleflight 的常用子集，
// 避免引入第三方依赖（项目零依赖风格）。
//
// 语义：
//   - 同 key 并发 → 只有首个执行 fn，其余阻塞等待其结果；
//   - fn 返回后删除 key，下一次调用重新执行（不缓存失败/成功）。
//   - fn 的 panic 会被捕获转为 error，保证等待者不会永久阻塞。
type flightGroup struct {
	mu sync.Mutex
	m  map[string]*flightCall
}

type flightCall struct {
	done chan struct{}
	val  interface{}
	err  error
}

// Do 执行 fn；若 key 已有在途调用则等待并复用其结果。
func (g *flightGroup) Do(key string, fn func() (interface{}, error)) (interface{}, error) {
	g.mu.Lock()
	if g.m == nil {
		g.m = make(map[string]*flightCall)
	}
	if c, ok := g.m[key]; ok {
		g.mu.Unlock()
		<-c.done // 首个调用有网络超时兜底，等待不会无限阻塞
		return c.val, c.err
	}
	c := &flightCall{done: make(chan struct{})}
	g.m[key] = c
	g.mu.Unlock()

	func() {
		defer func() {
			if r := recover(); r != nil {
				c.err = fmt.Errorf("chinacheck flight fn panic: %v", r)
			}
		}()
		c.val, c.err = fn()
	}()

	g.mu.Lock()
	delete(g.m, key)
	g.mu.Unlock()
	close(c.done)
	return c.val, c.err
}
