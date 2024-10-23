package geecache

import (
	"fmt"
	"log"
	"sync"
)

type Getter interface {
	Get(key string) ([]byte, error)
}

type GetterFunc func(key string) ([]byte, error)

func (f GetterFunc) Get(key string) ([]byte, error) {
	return f(key)
}

// Group 作为一个缓存的命名空间,比如缓存学生的成绩则名为scores
type Group struct {
	name      string // 缓存的名称
	getter    Getter // 缓存未命中时的回调函数
	mainCache cache  // 通过cache.go和lru目录实现的缓存
	peers     PeerPicker
}

var (
	mu     sync.RWMutex
	groups = make(map[string]*Group)
)

func NewGroup(name string, cacheBytes int64, getter Getter) *Group {
	if getter == nil {
		panic("nil Getter")
	}

	mu.Lock()
	defer mu.Unlock()

	g := &Group{
		name:      name,
		getter:    getter,
		mainCache: cache{cacheBytes: cacheBytes},
	}

	groups[name] = g
	return g
}

// GetGroup 用来获取特定名称的 Group
func GetGroup(name string) *Group {
	mu.RLock()
	defer mu.RUnlock()
	g := groups[name]
	return g
}

func (g *Group) Get(key string) (ByteView, error) {
	if key == "" {
		return ByteView{}, fmt.Errorf("查询的key为空")
	}

	if v, ok := g.mainCache.get(key); ok {
		log.Println("[GeeCache] 缓存命中!")
		return v, nil
	}

	// 如果没在本机缓存中查找到数据,通过load函数调用getLocally/getFromPeer，
	// getLocally通过用户提供的回调函数查找其他数据源，getFromPeer查找分布式缓存中的其他节点
	return g.load(key)
}

// 通过用户提供的回调函数,查询数据(用户可以根据需要,如果GeeCache缓存没命中,去访问如MySQL等数据源的数据)
func (g *Group) getLocally(key string) (ByteView, error) {
	bytes, err := g.getter.Get(key)
	if err != nil {
		return ByteView{}, err
	}

	value := ByteView{b: cloneBytes(bytes)}
	// 写回策略,从其他数据源访问到的数据,加入到缓存中
	g.populateCache(key, value)
	return value, nil
}

func (g *Group) populateCache(key string, value ByteView) {
	//log.Println("[执行缓存写回策略] 缓存 key:", key, "value:", value)
	g.mainCache.add(key, value)
}

func (g *Group) RegisterPeers(peers PeerPicker) {
	if g.peers != nil {
		panic("RegisterPeers函数被重复调用")
	}
	g.peers = peers
}

func (g *Group) load(key string) (ByteView, error) {
	if g.peers != nil {
		if peer, ok := g.peers.PickPeer(key); ok {
			if value, err := g.getFromPeer(peer, key); err == nil {
				return value, nil
			}
			log.Println("[GeeCache]从其他节点获取缓存值失败")
		}
	}
	return g.getLocally(key)
}

func (g *Group) getFromPeer(peer PeerGetter, key string) (ByteView, error) {
	bytes, err := peer.Get(g.name, key)
	if err != nil {
		return ByteView{}, err
	}
	return ByteView{b: bytes}, nil
}
