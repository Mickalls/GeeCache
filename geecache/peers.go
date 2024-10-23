package geecache

// PeerPicker 根据查询键 key 的值选取一个 peer (节点)
type PeerPicker interface {
	PickPeer(key string) (peer PeerGetter, ok bool)
}

// PeerGetter 从一个 group 中根据查询键 key 查询缓存值
type PeerGetter interface {
	// Get 从 group 查找缓存值
	Get(group string, key string) ([]byte, error)
}
