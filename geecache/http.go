package geecache

import (
	"GeeCache/consistenthash"
	pb "GeeCache/geecachepb"
	"fmt"
	"google.golang.org/protobuf/proto"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
)

const (
	defaultBasePath = "/_geecache/"
	defaultReplicas = 50
)

type HTTPPool struct {
	self        string                 // 记录自己的地址(主机IP、端口)
	basePath    string                 // 节点之间通信的前缀,默认为 /_geecache/
	mu          sync.Mutex             // 支持管理
	peers       *consistenthash.Map    // 一致性哈希算法对应的Map对象
	httpGetters map[string]*httpGetter // 映射远程节点名字到对应的httpGetter
}

// NewHTTPPool 初始化一个HTTPPool
func NewHTTPPool(self string) *HTTPPool {
	return &HTTPPool{
		self:     self,
		basePath: defaultBasePath,
	}
}

// Log 用于输出日志
func (p *HTTPPool) Log(format string, v ...interface{}) {
	log.Printf("[server %s] %s", p.self, fmt.Sprintf(format, v...))
}

func (p *HTTPPool) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// 判断前缀是否是 basePath
	if !strings.HasPrefix(r.URL.Path, p.basePath) {
		panic("HTTPPool serving unexpected path:" + r.URL.Path)
	}
	p.Log("%s %s", r.Method, r.URL.Path)
	parts := strings.SplitN(r.URL.Path[len(p.basePath):], "/", 2)
	if len(parts) != 2 {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	// 约定访问路径格式为 /<basePath>/<groupName>/<key>
	groupName := parts[0]
	key := parts[1]

	group := GetGroup(groupName)
	if group == nil {
		http.Error(w, "Group not found", http.StatusNotFound)
		return
	}

	view, err := group.Get(key)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Write(view.ByteSlice())
}

// Set HTTPPool 对象的 peers (peer对应远程节点) 一致性哈希算法初始化
func (p *HTTPPool) Set(peers ...string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	// 引用并创建一致性哈希算法对应的对象
	p.peers = consistenthash.New(defaultReplicas, nil)
	// 将所有真实节点添加进去
	p.peers.Add(peers...)

	p.httpGetters = make(map[string]*httpGetter, len(peers))
	for _, peer := range peers {
		p.httpGetters[peer] = &httpGetter{baseURL: peer + p.basePath}
	}
}

// PickPeer 包装了一致性哈希算法的Get函数,选择节点并返回节点对应的httpGetter对象
func (p *HTTPPool) PickPeer(key string) (PeerGetter, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if peer := p.peers.Get(key); peer != "" && peer != p.self {
		p.Log("选择节点:%v", peer)
		return p.httpGetters[peer], true
	}
	return nil, false
}

// 确保 HTTPPool 类型实现了 PeerPicker 接口
var _ PeerPicker = (*HTTPPool)(nil)

// httpGetter 对应一个远程节点
type httpGetter struct {
	baseURL string // 远程节点的地址
}

// Get 客户端根据节点名称group和键key从其他节点查询缓存结果
func (h *httpGetter) Get(in *pb.Request, out *pb.Response) error {
	// 根据url格式,构建目的地的url,QueryEscape函数对string编码,将特殊字符转换为url允许的格式
	u := fmt.Sprintf(
		"%v%v/%v",
		h.baseURL,
		url.QueryEscape(in.GetGroup()),
		url.QueryEscape(in.GetKey()),
	)
	res, err := http.Get(u)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("http get error: '%s' from server", res.Status)
	}

	bytes, err := io.ReadAll(res.Body)
	if err != nil {
		return fmt.Errorf("reading response body: %v", err)
	}

	if err = proto.Unmarshal(bytes, out); err != nil {
		return fmt.Errorf("unmarshaling response body: %v", err)
	}

	return nil
}

// 确保 httpGetter 类型实现了 PeerGetter 接口
var _ PeerGetter = (*httpGetter)(nil)
