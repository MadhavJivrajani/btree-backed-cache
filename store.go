package main

import (
	"fmt"

	"github.com/google/btree"
	"k8s.io/client-go/tools/cache"
)

type BTreeStore struct {
	// TODO: make things thread safe.
	n    int
	tree *btree.BTree
}

func NewBTreeStore(degree int) *BTreeStore {
	return &BTreeStore{
		tree: btree.New(degree),
	}
}

func (t *BTreeStore) Add(obj interface{}) error {
	return t.addOrUpdate(obj, true)
}

func (t *BTreeStore) Update(obj interface{}) error {
	return t.addOrUpdate(obj, false)
}

func (t *BTreeStore) Delete(obj interface{}) error {
	storeElem, ok := obj.(storeElement)
	if !ok {
		return fmt.Errorf("obj not a storeElement: %#v", obj)
	}
	item := t.tree.Delete(storeElem)
	if item == nil {
		return fmt.Errorf("obj does not exist")
	}
	t.n--

	return nil
}

func (t *BTreeStore) List() []interface{} {
	items := make([]interface{}, 0, t.n)
	t.tree.Ascend(func(i btree.Item) bool {
		items = append(items, i.(interface{}))
		return true
	})

	return items
}

func (t *BTreeStore) ListKeys() []string {
	items := make([]string, 0, t.n)
	t.tree.Ascend(func(i btree.Item) bool {
		items = append(items, i.(storeElement).Key)
		return true
	})

	return items
}

func (t *BTreeStore) Get(obj interface{}) (item interface{}, exists bool, err error) {
	storeElem, ok := obj.(storeElement)
	if !ok {
		return nil, false, fmt.Errorf("obj is not a storeElement")
	}
	item = t.tree.Get(storeElem)
	if item == nil {
		return nil, false, nil
	}
	return item, false, nil
}

func (t *BTreeStore) GetByKey(key string) (item interface{}, exists bool, err error) {
	t.tree.Ascend(func(i btree.Item) bool {
		if key == i.(storeElement).Key {
			item = i
			exists = true
			return false
		}
		return true
	})
	return item, exists, nil
}

// no-op
func (t *BTreeStore) Replace([]interface{}, string) error {
	return nil
}

// no-op
func (t *BTreeStore) Resync() error {
	return nil
}

func (t *BTreeStore) ListSince(rv uint64) []interface{} {
	items := []interface{}{}
	t.tree.AscendGreaterOrEqual(storeElement{Rv: rv}, func(i btree.Item) bool {
		if rv <= i.(storeElement).Rv {
			items = append(items, i.(interface{}))
			return true
		}
		return false
	})

	return items
}

func (t *BTreeStore) addOrUpdate(obj interface{}, isAdd bool) error {
	// A nil obj cannot be entered into the btree,
	// results in panic.
	if obj == nil {
		return fmt.Errorf("obj cannot be nil")
	}
	storeElem, ok := obj.(storeElement)
	if !ok {
		return fmt.Errorf("obj not a storeElement: %#v", obj)
	}
	item := t.tree.ReplaceOrInsert(storeElem)
	if isAdd {
		isSame := item.(storeElement).Rv == storeElem.Rv
		if !isSame {
			t.n++
		}
	}

	return nil
}

// TODO: The goal is to make a cache.Indexer
// Immediate goal is to make a cache.Store
var _ cache.Store = (*BTreeStore)(nil)
