package geecache

import (
	"GeeCache/lru"
	"sync"
)

// 实例化lru，封装get和add方法，并添加互斥锁mu，支持单机并发缓存
type cache struct {
	mu         sync.Mutex // 添加互斥锁,使其支持并发
	lru        *lru.Cache // 封装lru.Cache缓存对象
	cacheBytes int64      // 代表lru.Cache对象允许使用的最大内存
}

// 封装 LRU 的 ADD 方法, 并添加互斥锁
func (c *cache) add(key string, value ByteView) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lru == nil {
		c.lru = lru.New(c.cacheBytes, nil)
	}
	c.lru.Add(key, value)
}

// 封装LRU 的 GET 方法，并添加互斥锁
func (c *cache) get(key string) (ByteView, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.lru == nil {
		return ByteView{}, false
	}
	if val, ok := c.lru.Get(key); ok {
		return val.(ByteView), ok
	}
	return ByteView{}, false
}
