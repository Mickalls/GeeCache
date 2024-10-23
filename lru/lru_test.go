package lru

import (
	"reflect"
	"testing"
)

type MyString string // 不能为非本地的类型定义方法，只能为自己定义的类型或者在当前包中定义的类型添加方法

func (d MyString) Len() int {
	return len(d)
}

func TestGet(t *testing.T) {
	lru := New(int64(0), nil)
	lru.Add("key1", MyString("1234"))
	if v, ok := lru.Get("key1"); !ok || string(v.(MyString)) != "1234" {
		t.Fatalf("缓存命中{\"key1\":\"1234\"失败}")
	}
	if _, ok := lru.Get("key2"); ok {
		t.Fatalf("缓冲命中失效key")
	}
}

func TestRemoveoldest(t *testing.T) {
	k1, k2, k3 := "key1", "key2", "key3"
	v1, v2, v3 := "value1", "value2", "value3"
	cap := len(k1 + k2 + v1 + v2)
	lru := New(int64(cap), nil)
	lru.Add(k1, MyString(v1))
	lru.Add(k2, MyString(v2))
	lru.Add(k3, MyString(v3))
	if _, ok := lru.Get("key1"); ok || lru.Len() != 2 {
		t.Fatalf("LRU淘汰策略错误")
	}
}

func TestOnEvicted(t *testing.T) {
	keys := make([]string, 0)
	callback := func(key string, value Value) {
		keys = append(keys, key)
	}
	lru := New(int64(10), callback)
	lru.Add("key1", MyString("123456")) // 10 bytes
	lru.Add("k2", MyString("k2"))       // 4 bytes
	lru.Add("k3", MyString("k3"))       // 4 bytes
	lru.Add("k4", MyString("k4"))       // 4 bytes

	expect := []string{"key1", "k2"}

	if !reflect.DeepEqual(expect, keys) {
		t.Fatalf("回调函数测试失败,期望返回的keys数组应等于 %s 却得到了 %s", expect, keys)
	}
}
