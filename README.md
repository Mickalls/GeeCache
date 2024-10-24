# GeeCache
### 前情提要
项目原地址：[7天用Go从零实现分布式缓存GeeCache](https://geektutu.com/post/geecache.html)

### 架构图
主要围绕Day5的测试用例，画的架构图
![image.png](https://typora-note-photos.oss-cn-guangzhou.aliyuncs.com/20241024183830.png)


### 关键词/收获
**Day1：实现LRU策略**
- LRU算法
- go test 测试
 
**Day2：单机并发缓存**
- 互斥锁
- 接口型函数
- 封装思想

**Day3：HTTP Server**
- go的http标准库
- URL设计
- 处理http请求

**Day4：一致性哈希**
- 一致性哈希算法实现

**Day5：搭建HTTP Client、构建分布式节点**
- 一致性哈希算法应用
- 良好的接口设计思想



## 详细笔记
### Day1 - LRU
1. 定义基础 LRU 缓存对象的数据结构
2. 实现相关函数
#### LRU 缓存对象的数据结构
```go
type Cache struct {
	maxBytes  int64                         // 允许使用的最大内存
	nbytes    int64                         // 当前已使用的内存
	ll        *list.List                    // 双向链表
	cache     map[string]*list.Element      // 哈希表：记录 key 到 链表节点指针 的映射
	OnEvicted func(key string, value Value) // 某条记录被移除后执行的回调函数,可以为nil
}
```

双向链表节点的数据类型(`list.Element`的类型)：
```go
type entry struct {
	key   string  // 键的类型只为string
	value Value   // Value 接口作为键存储的值
}
```

`Value`接口
```go
type Value interface {
	Len() int
}
```

#### LRU 缓存对象的相关函数
lru.Cache的`New`方法：实例化缓存对象
- 参数：（该缓存对象可存储的最大字节数，删除元素的回调函数）
- 返回：新的缓存对象
```go
func New(maxBytes int64, onEvicted func(string, Value)) *Cache { ... }
```

lru.Cache的`Get`方法：查询缓存中键对应的值
- 参数：key
- 返回：value，布尔标志
```go
func (c *Cache) Get(key string) (value Value, ok bool) { ... }
```

lru.Cache的`Add`方法：新增/修改
- 参数：key，value
- 注意：记得更新当前缓存已使用的内存值，即`c.nbytes`
```go
func (c *Cache) Add(key string, value Value) { ... }
```

lru.Cache的`Len`方法：
- 实现接口`Value`的函数
```go
func (c *Cache) Len()
```

### Day2 - 单机并发缓存
1. 抽象了一个只读数据结构——`ByteView`表示缓存值：使得读操作不影响原数据，更加安全
2. 为`lru.Cache`缓存对象添加并发特性：封装该类型所拥有的函数和其本身的数据结构为`geecache/cache.go`中的`cache`结构体，利用互斥锁使其在被并发访问时读写操作安全
3. 为满足与外部交互（分布式架构中其他缓存节点）、独立开不同的缓存对象（可以为每个缓存对象命名来区分），接着将`cache`结构体封装为本项目核心组件之一的`Group`结构体
4. 步骤3中，需要写一个回调函数的接口——`Getter`

##### ByteView实现接口Value
`ByteView`的结构体只包含了字节数组，用来存储缓存的值
```go
type ByteView struct {
	b []byte
}
```

为了匹配上缓存值接口`Value`，实现该接口含有的函数`Len`
```go
func (v ByteView) Len() int {
	return len(v.b)
}
```

再实现深拷贝函数，使得读操作得到的数据是副本，这样读操作就更加安全了
```go
func (v ByteView) ByteSlice() []byte {
	return cloneBytes(v.b)
}

func cloneBytes(b []byte) []byte {
	c := make([]byte, len(b))
	copy(c, b)
	return c
}
```

还有个将字节数组转化为string的工具函数，至此，ByteView表示缓存值的任务就完成了
```go
func (v ByteView) String() string {
	return string(v.b)
}
```


#### 封装lru.Cache为cache.go/cache：添加并发特性
- **封装 LRU 缓存对象结构体**
```go
type cache struct {
	mu         sync.Mutex // 添加互斥锁,使其支持并发
	lru        *lru.Cache // 封装lru.Cache缓存对象
	cacheBytes int64      // 代表lru.Cache对象允许使用的最大内存
}
```

- **封装Add函数**
  注意，封装后的add函数，参数value值的类型不再是Value接口，而是封装后的`ByteView`
```go
func (c *cache) add(key string, value ByteView) {
	c.mu.Lock() // 同一时刻只允许单一线程访问缓存，使得并发操作安全进行
	defer c.mu.Unlock()
	if c.lru == nil {
		c.lru = lru.New(c.cacheBytes, nil)
	}
	c.lru.Add(key, value)
}
```

- **封装Get函数**
  注意，封装后的get函数，返回的value值的类型是封装后的`ByteView`
```go
func (c *cache) get(key string) (ByteView, bool) {
	c.mu.Lock() // 同一时刻只允许单一线程访问缓存，使得并发操作安全进行
	defer c.mu.Unlock()
	if c.lru == nil {
		return ByteView{}, false
	}
	if val, ok := c.lru.Get(key); ok {
		return val.(ByteView), ok
	}
	return ByteView{}, false
}
```


#### 封装cache.go/cache为geecache.go/Group：添加与外部交互、区分不同缓存对象的功能
##### Group 结构体
这里`Getter`是一个接口
```go
type Group struct {
	name      string // 缓存的名称
	getter    Getter // 缓存未命中时的回调函数
	mainCache cache  // 封装cache.go/cache结构体
}
```

##### **Getter接口**
>Q1：为什么它是接口，注释里又说它是回调函数？
>A：go语言特性——接口型函数
>Q2：为什么它要是接口？
>A：通过用户提供方法，这样用户可以根据需要自定义函数内逻辑。
>Q2：为什么Group结构体需要有缓存未命中的回调函数
>A：要想一想，缓存未命中时，需要做什么呢？比如使用了`Redis`+`MySQL`两个技术栈的项目，如果`Redis`未命中，那么肯定下一步是要去`MySQL`查找数据，找到了数据后，并写回`Redis`；作为分布式缓存，还会存在其他缓存节点有可能存储了这个缓存值，所以还要去其他节点搜索缓存中的值。因此，==本地缓存未命中==的时候的任务：（1）查询分布式中其他缓存节点是否存储对应缓存值（2）查询下一个数据源（3）如果得到查询值，回写到缓存。但是，因为==“下一个数据源”==并不是固定的，所以这部分需要==用户提供逻辑来灵活地满足具体场景要求==。而（1）和（3）的任务是可以直接固定实现的，因此，将步骤（2）抽象为`Getter`接口。

总结：Getter接口是为了让用户在分布式缓存未命中时，灵活地去查询下一个数据源。
```go
type Getter interface {
	Get(key string) ([]byte, error)
}

type GetterFunc func(key string) ([]byte, error)

func (f GetterFunc) Get(key string) ([]byte, error) {
	return f(key)
}
```


##### Group相关函数
- 初始化函数和通过Group的名称获取对应group
  *Getter不可以为nil，否则报panic*
```go
var (
	mu     sync.RWMutex              // 保护groups对象的并发安全
	groups = make(map[string]*Group) // 记录所有的Group对象，通过名称映射到Group指针
)
func NewGroup(name string, cacheBytes int64, getter Getter) *Group { ... }
func GetGroup(name string) *Group { ... }
```

- 封装`cache.go`的`Get`函数：
  如果没在本机缓存中查找到数据,通过load函数调用（1）getFromPeer （2）getLocally （3）pupolateCache
  （1）getFromPeer：从分布式缓存的其他节点查询（在Day3+Day4+Day5会实现）
  （2）getLocally：调用用户提供的回调函数，查询下一个数据源
  （3）populateCache：回写函数，在步骤（1）或（2）查询成功后，将缓存值回写回本地的缓存系统中
```go
func (g *Group) Get(key string) (ByteView, error) {
	if key == "" {
		return ByteView{}, fmt.Errorf("查询的key为空")
	}

	if v, ok := g.mainCache.get(key); ok {
		log.Println("[GeeCache] 缓存命中!")
		return v, nil
	}
	return g.load(key)
}
```




### Day3 - 搭建分布式缓存节点的HTTP Server功能
HTTP Server的功能其实就是代表了可以别分布式其他节点访问的功能，那么主要就是实现接受请求并处理请求，而分布式缓存中每个节点接收到的请求无非就是查询某个键对应的值是否被缓存了当前节点，那么就只需要查询并响应结果即可。

步骤：
1. 创建本项目核心组件之一的`HTTPPool`结构体：承载节点间HTTP通信任务的核心数据结构（包括服务端和客户端，Day3实现服务端）
2. 的


#### HTTPPool 数据结构
*目前的HTTPPool结构体还并不是完整实现，为实现服务端功能暂时结构如下*
```go
type HTTPPool struct {
	self        string // 记录自己的地址(主机IP、端口)  
	basePath    string // 节点之间通信的前缀,默认为 /_geecache/
}
```

初始化HTTPPool对象：
```go
const defaultBasePath = "/_geecache/"
func NewHTTPPool(self string) *HTTPPool { ... }
```

#### HTTPPool实现ServeHTTP函数
实现了这个函数后，HTTPPool结构体对象，就可以作为`handler`类型参数，传递给`go`的`http`标准库的`ListenAndServe`函数中的`handler`类型参数

ServeHTTP的内部处理逻辑：
1. 判断接收到的HTTP请求的URL的前缀是否匹配`HTTPPool`的`basePath`参数（节点之间通信的前缀）
2. 日志输出：接收到的请求类型和请求源地址
3. 检查请求URL的路径是否符合规则：`/<basePath>/<groupname>/<query_key>`
4. 提取`<groupname>`和`<query_key>`
5. 根据`<groupname>`查询本地节点对应的`Group`对象
6. 通过`Group`结构体的`Get`方法，查询缓存是否命中
7. 将查询结构通过深拷贝写入响应体
```go
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
```



### Day4 - 一致性哈希
#### 理论知识
实现一致性哈希算法是从单机走向分布式的重要环节，通过这个算法可以解决以下问题：
- Q1：本地缓存没命中，查询其他分布式架构中的节点时，该选择哪个节点，才能更大概率的命中呢？
- Q2：分布式架构中的节点数量变化，某一存储了大量数据的节点宕机，引发缓存雪崩，怎么办？

一致性哈希算法解决方法：
- 将`key`映射到 $2^{32}$ 的空间中，将这个数字首尾相连，形成一个环
- 计算每一个节点（or 虚拟节点）的哈希值，放置在环上（==放置在环上的是节点而不是查询键!!==）
- 查询键时，计算键对应的哈希值，从该点出发==寻找顺时针找到的第一个节点==，选取该节点作为查询的机器

这样子，如果分布式缓存的Server，接收到很多次同一个`key`的查询，每次都会查询同一个节点，第一次会在该节点没命中并最终回写该缓存值，从第二次开始每次这个`key`的查询，都会根据一致性哈希算法发往第一次接受这个查询的节点，从而每次都能查询到之前回写记录在该节点的缓存值

>Q：为什么要有虚拟节点？
>A：每个真实节点都对应若干个虚拟节点，如果服务器的节点过少，也会有数据倾斜问题，即一个节点被分配到的缓存记录数据过大（比如环上两节点相近且只有两节点的话），该节点宕机也有可能导致缓存雪崩，因此，可以选择构建虚拟节点，每次根据一致性哈希算法，计算到的节点，如果是虚拟节点就匹配其对应的真实节点，分散开所以缓存数据，避免数据倾斜风险。

#### 一致性哈希的数据结构
```go
type Map struct {
	hash     Hash           // 哈希计算函数，默认crc32.ChecksumIEEE算法
	replicas int            // 虚拟节点倍数(1个真实节点可以拥有replicas个虚拟节点)
	keys     []int          // 哈希环上所有的节点的哈希值(根据哈希值大小从小到大排列从而形成环)
	hashMap  map[int]string // key-虚拟节点的哈希值 value-真实节点的名称
}
```


#### 一致性哈希的相关函数
- 初始化函数：`New`
```go
func New(replicas int, fn Hash) *Map { ... }
```

- 添加真实节点：`Add`
  会根据初始化时设定的`replicas`创建对应的虚拟节点
1. 遍历所有的真实节点的名称
2. 对于每一个真实节点，创建`replicas`个虚拟节点
    1. `m.keys` 记录节点哈希值
    2. `m.hashMap` 记录从节点哈希值到节点名称的映射
3. 排序`m.keys`，形成哈希环
```go
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
```


- `Get`函数：根据查询键，利用一致性哈希算法，查询该查询键所匹配到的真实节点，返回该节点的名称，用于后续向该节点发送请求查询缓存是否命中
1. 空的查询键不执行一致性哈希计算
2. 通过哈希函数，计算查询键的哈希值
3. 二分查找，匹配哈希值对应哈希环上顺时针方向第一个节点
4. 根据`hashMap`记录的映射，得到最终匹配到的节点的名称，并返回
```go
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
```


### Day5 - 搭建分布式缓存节点的HTTP Client功能
回顾一下，Day3实现了HTTP Server功能，处理http请求并执行缓存查询；Day4实现了一致性哈希算法，解决分布式架构中节点选择和数据倾斜的问题。在Day5，接着完成HTTP Client的功能，？

步骤：
1. 定义接口`PeerPicker`和`PeerGetter`
2. 结构体`httpGetter`，实现接口`PeerGetter`
3. 为`HTTPPool`添加选择节点的功能，实现接口`PeerPicker`
4. 扩展缓存未命中时的工作流程，添加“查询分布式中其他节点”的步骤

按照步骤实现后，每一个节点都具有了分布式节点的基本功能。Day5相对难理解一点，接下来对于每一步骤都详细解析

#### 定义接口`PeerPicker`和`PeerGetter`
首先通过名字来理解，接口`PeerPicker`代表了一种具有选择节点功能的类型；接口`PeerGetter`代表了分布式中非本地的节点相对于本地当前节点的一种抽象，本地节点可以通过实现了`PeerGetter`接口的类型的对象，来在本地向这个对象对应的远程节点发起查询请求。

```go
type PeerPicker interface {
	// PickPeer 根据查询键 key 的值选取一个 peer (节点)
	PickPeer(key string) (peer PeerGetter, ok bool)
}

type PeerGetter interface {
	// Get 从 group 查找key键对应缓存值
	Get(group string, key string) ([]byte, error)
}
```

#### 创建结构体`httpGetter`并实现接口`PeerGetter`
`httpGetter`的任务：通过一致性哈希算法选择出的目标节点并返回的名称，构建访问目标节点所需的URL，并提供向该URL发送http GET请求的功能，最终返回响应数据或错误

http结构体很简单，只存储一个`baseURL`，代表了远程节点的地址。
它也只实现了`PeerGetter`接口中的`get`函数：==通过一致性哈希算法选择的Group对象名称???==
1. 根据`baseURL`和参数中的`Group`名称以及`key`查询键的值，拼接出最终发起http请求的URL
2. 通过http标准库，发起http GET请求
3. 判断响应状态是否正常
4. 读取响应体中的数据（字节数组）
5. 返回字节数组，error（正常为nil）
```go
func (h *httpGetter) Get(group string, key string) ([]byte, error) {
	// 根据url格式,构建目的地的url,QueryEscape函数对string编码,将特殊字符转换为url允许的格式
	u := fmt.Sprintf(
		"%v%v/%v",
		h.baseURL,
		url.QueryEscape(group),
		url.QueryEscape(key),
	)
	res, err := http.Get(u)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http get error: '%s' from server", res.Status)
	}

	bytes, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body: %v", err)
	}
	
	return bytes, nil
}
```



#### 为`HTTPPool`添加选择节点功能，实现接口`PeerPicker`
- 扩展`HTTPPool`的数据结构，新增三个成员变量：
1. `mu`：（1）给peers成员变量添加节点时加锁（2）查询其他分布式peers，选取peer时加锁
2. `peers`：代表了所有分布式节点，通过一致性哈希算法维护
3. `httpGetters`：维护远程节点名称到对应`httpGetter`对象的映射
```go
type HTTPPool struct {
	self        string                 // 记录自己的地址(主机IP、端口)
	basePath    string                 // 节点之间通信的前缀,默认为 /_geecache/
	mu          sync.Mutex             // 并发控制
	peers       *consistenthash.Map    // 一致性哈希算法对应的Map对象
	httpGetters map[string]*httpGetter // 映射远程节点名字到对应的httpGetter
}
```

- 初始化一致性哈希算法
  既然`peers`通过一致性哈希算法维护，那就需要有初始化一致性哈希算法的方法。初始化的过程，主要就是设置节点到哈希环上，并将映射存储到`httpGetters`上
```go
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
```

- 实现`PeerPicker`接口：实现`PickPeer`函数
  利用一致性哈算法的`Get`函数，实现选取节点的功能。因为得到的是节点对应名称，所以还需通过`httpGetters`映射得到节点的`httpGetters`对象，并返回
```go
func (p *HTTPPool) PickPeer(key string) (PeerGetter, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if peer := p.peers.Get(key); peer != "" && peer != p.self {
		p.Log("选择节点:%v", peer)
		return p.httpGetters[peer], true
	}
	return nil, false
}
```


#### 扩展缓存未命中的工作流程
在Day2实现`Group`对象时，详细分析了缓存未命中时的具体任务，但当时只实现了用户自定义的回调函数Getter和回写函数，还没实现分布式核心功能“查询其他节点是否有该缓存”。通过Day3直至现在的努力，其实已经具有该功能了，只需要添加到缓存未命中的工作流程中。

具体的，我们添加到load函数中。
当`Group`对象的`Get`方法未命中时（代表本机缓存未命中），就在`load`函数中，先后执行“查询分布式节点”和“查询下一数据源”，并最终回写本机缓存。
```go
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
```

但是，看到代码你会疑惑，`Group`对象没有`peers`成员变量啊？
是的，所以还需要扩展`Group`对象的成员变量，并实现在`Group`对象上注册节点的功能

**扩展后的`Group`**
你会发现，其实`peers`的类型应当也是`PeerPicker`接口，才能满足peers具有分布式中选择节点的需求。==而前面`HTTPPool`结构体已经实现了`PeerPicker`接口==，所以==注册节点的过程，就是将`NewHTTPPool`得到的`HTTPPool`对象设置给`Group`对象的`peers`变量==
```go
type Group struct {
	name      string // 缓存的名称
	getter    Getter // 缓存未命中时的回调函数
	mainCache cache  // 通过cache.go和lru目录实现的缓存
	peers     PeerPicker // HTTPPool 结构体实现了 PeerPicker 接口,所以注册节点只需将HTTPPool注册到Group的peers变量
}
```

**注册节点函数**
```go
func (g *Group) RegisterPeers(peers PeerPicker) {
	if g.peers != nil {
		panic("RegisterPeers函数被重复调用")
	}
	g.peers = peers
}
```




### 附件
完成Day1-Day5后的测试截图：
- Server通过PickPeer选取到了一致性哈希对应的Client节点，向该节点查询缓存，该节点第一次会从用户自定义的数据源查找数据，然后并在该节点内回写缓存，然后返回查询结果给Server
- 之后每次Server都通过该键访问之前的Client节点，就缓存命中了
  ![image.png](https://typora-note-photos.oss-cn-guangzhou.aliyuncs.com/20241024173525.png)
