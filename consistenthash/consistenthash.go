package consistenthash

import (
	"hash/crc32"
	"sort"
	"strconv"
)

// Hash 函数类型,哈希计算函数,默认crc32...算法,允许替换为自定义哈希函数
type Hash func(data []byte) uint32

type Map struct {
	hash     Hash           // 哈希计算函数，默认crc32.ChecksumIEEE算法
	replicas int            // 虚拟节点倍数(1个真实节点可以拥有replicas个虚拟节点)
	keys     []int          // 哈希环
	hashMap  map[int]string // key-虚拟节点的哈希值 value-真实节点的名称
}

func New(replicas int, fn Hash) *Map {
	m := &Map{
		hash:     fn,
		replicas: replicas,
		hashMap:  make(map[int]string),
	}
	if m.hash == nil {
		m.hash = crc32.ChecksumIEEE
	}
	return m
}

// Add 添加真实节点/机器
func (m *Map) Add(keys ...string) {
	for _, key := range keys {
		// 对于每一个真实节点的key,对应创建 m.replicas 个虚拟节点,约定虚拟节点名称为 "i" + key
		for i := 0; i < m.replicas; i++ {
			hash := int(m.hash([]byte(strconv.Itoa(i) + key)))
			m.keys = append(m.keys, hash)
			m.hashMap[hash] = key
		}
	}
	sort.Ints(m.keys)
}

// Get 根据一致性哈希算法,计算key的哈希值,匹配相应节点(顺时针找到下一个节点),最终得到缓存所在机器的名称
func (m *Map) Get(key string) string {
	if len(m.keys) == 0 {
		return ""
	}
	hash := int(m.hash([]byte(key)))
	idx := sort.Search(len(m.keys), func(i int) bool {
		return m.keys[i] >= hash
	})
	// 注意, idx 可能为 len(m.keys),此时应该对应的下表应为 0
	// 哈希环是环状, 对于查询的key哈希值比最后一个节点的哈希值还大的情况，映射到哈希值最小的节点
	return m.hashMap[m.keys[idx%len(m.keys)]]
}
