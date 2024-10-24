package singleflight

import "sync"

// call 代表进行中或已结束的请求.wg锁避免重入
type call struct {
	wg  sync.WaitGroup
	val interface{}
	err error
}

// Group 是singleflight的主数据结构，管理不同key的请求(call)
type Group struct {
	mu sync.Mutex // 保护 m 的并发安全
	m  map[string]*call
}

func (g *Group) Do(key string, fn func() (interface{}, error)) (interface{}, error) {
	// 加锁保护 g.m 不被并发读写
	g.mu.Lock()
	// 如果还未初始化就初始化
	if g.m == nil {
		g.m = make(map[string]*call)
	}
	// 如果查询的key存在进行中/已结束的请求，等待该call对象的wg释放，释放后就得到了查询key对应的val
	if c, ok := g.m[key]; ok {
		g.mu.Unlock()
		c.wg.Wait()
		return c.val, c.err
	}
	// 这个key还没有被查询过，新增一次查询并记录在g.m中
	c := new(call)
	// 避免同一key短时间内发起多次请求
	c.wg.Add(1)
	// 记录 key 到 call 对象的映射
	g.m[key] = c
	// 对 g.m 的读写操作结束，释放锁
	g.mu.Unlock()

	c.val, c.err = fn()
	// 查询完 key 后释放call对象的wg锁
	c.wg.Done()

	// 查询完后，为了数据一致性，要从 g.m 中删去前面的记录
	// 所以要加锁
	g.mu.Lock()
	delete(g.m, key)
	g.mu.Unlock()

	return c.val, c.err
}
