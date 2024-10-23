package geecache

import (
	"fmt"
	"log"
	"reflect"
	"testing"
)

func TestGetter(t *testing.T) {
	var f Getter = GetterFunc(func(key string) ([]byte, error) {
		return []byte(key), nil
	})
	expect := []byte("key")

	if v, _ := f.Get("key"); !reflect.DeepEqual(v, expect) {
		t.Errorf("expect %v, got %v", expect, v)
	}
}

var db = map[string]string{
	"Tom":  "630",
	"Jack": "589",
	"Sam":  "567",
}

func TestGet(t *testing.T) {
	loadCounts := make(map[string]int, len(db))
	gee := NewGroup("scores", 2<<10, GetterFunc(func(key string) ([]byte, error) {
		log.Printf("[缓存没查到key:%s的数据,在本机自定义的数据源搜索\n", key)
		if v, ok := db[key]; ok {
			if _, ok := loadCounts[key]; !ok {
				loadCounts[key] = 0
			}
			loadCounts[key]++
			log.Println("key:", key, "count:", loadCounts[key])
			return []byte(v), nil
		}
		log.Printf("key:%s 不存在！\n", key)
		return nil, fmt.Errorf("key:%s 不存在", key)
	}))

	for k, v := range db {
		if view, err := gee.Get(k); err != nil || view.String() != v {
			t.Fatalf("获取 key:%s 的值失败", k)
		}
		if _, err := gee.Get(k); err != nil || loadCounts[k] > 1 {
			t.Fatalf("key:%s 的缓存失效", k)
		}
		// 否则就缓存命中
	}

	if view, err := gee.Get("unknow"); err == nil {
		t.Fatalf("使用不存在的键查询缓存,获得到了非法值:%s", view)
	}
}
