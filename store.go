package main

import (
	"fmt"
	"strings"
	"sync"

	"github.com/google/btree"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/cache"
)

type btreeIndexer interface {
	cache.Store
	ByIndex(indexName, indexValue string) ([]interface{}, error)
	Clone() btreeIndexer
	LimitPrefixRead(limit int64, key string) []interface{}
}

type btreeStore struct {
	lock     sync.RWMutex
	tree     *btree.BTree
	indices  cache.Indices
	indexers cache.Indexers
	keyFunc  cache.KeyFunc
}

func newBtreeStore(keyFunc cache.KeyFunc, indexers cache.Indexers, degree int) *btreeStore {
	return &btreeStore{
		tree:     btree.New(degree),
		indices:  cache.Indices{},
		indexers: indexers,
		keyFunc:  keyFunc,
	}
}

func (t *btreeStore) Add(obj interface{}) error {
	t.lock.Lock()
	defer t.lock.Unlock()

	return t.addOrUpdateLocked(obj)
}

func (t *btreeStore) Update(obj interface{}) error {
	t.lock.Lock()
	defer t.lock.Unlock()

	return t.addOrUpdateLocked(obj)
}

func (t *btreeStore) Delete(obj interface{}) error {
	t.lock.Lock()
	defer t.lock.Unlock()

	storeElem, ok := obj.(*storeElement)
	if !ok {
		return fmt.Errorf("obj not a storeElement: %#v", obj)
	}

	key, err := t.keyFunc(obj)
	if err != nil {
		return cache.KeyError{Obj: obj, Err: err}
	}
	err = t.updateIndicesLocked(obj, nil, key)
	if err != nil {
		return err
	}

	item := t.tree.Delete(storeElem)
	if item == nil {
		return fmt.Errorf("obj does not exist")
	}

	return nil
}

func (t *btreeStore) List() []interface{} {
	t.lock.RLock()
	defer t.lock.RUnlock()

	items := make([]interface{}, 0, t.tree.Len())
	t.tree.Ascend(func(i btree.Item) bool {
		items = append(items, i.(interface{}))
		return true
	})

	return items
}

func (t *btreeStore) ListKeys() []string {
	t.lock.RLock()
	defer t.lock.RUnlock()

	items := make([]string, 0, t.tree.Len())
	t.tree.Ascend(func(i btree.Item) bool {
		items = append(items, i.(*storeElement).Key)
		return true
	})

	return items
}

func (t *btreeStore) Get(obj interface{}) (item interface{}, exists bool, err error) {
	t.lock.RLock()
	defer t.lock.RUnlock()

	storeElem, ok := obj.(*storeElement)
	if !ok {
		return nil, false, fmt.Errorf("obj is not a storeElement")
	}
	item = t.tree.Get(storeElem)
	if item == nil {
		return nil, false, nil
	}

	return item, true, nil
}

func (t *btreeStore) GetByKey(key string) (item interface{}, exists bool, err error) {
	t.lock.RLock()
	defer t.lock.RUnlock()

	return t.getByKeyLocked(key)
}

func (t *btreeStore) Replace(objs []interface{}, _ string) error {
	t.lock.Lock()
	defer t.lock.Unlock()

	t.tree.Clear(false)
	for _, obj := range objs {
		err := t.addOrUpdateLocked(obj)
		if err != nil {
			return err
		}
	}

	return nil
}

func (t *btreeStore) Resync() error {
	// Nothing to do.
	return nil
}

func (t *btreeStore) Clone() btreeIndexer {
	t.lock.Lock()
	defer t.lock.Unlock()

	return &btreeStore{tree: t.tree.Clone()}
}

// addOrUpdateLocked assumes a lock is held and is used for Add
// and Update operations.
func (t *btreeStore) addOrUpdateLocked(obj interface{}) error {
	// A nil obj cannot be entered into the btree,
	// results in panic.
	if obj == nil {
		return fmt.Errorf("obj cannot be nil")
	}
	storeElem, ok := obj.(*storeElement)
	if !ok {
		return fmt.Errorf("obj not a storeElement: %#v", obj)
	}

	returned := t.tree.ReplaceOrInsert(storeElem)
	key, err := t.keyFunc(obj)
	if err != nil {
		return cache.KeyError{Obj: obj, Err: err}
	}
	if returned == nil {
		return t.updateIndicesLocked(nil, obj, key)
	}

	old := returned.(interface{})
	return t.updateIndicesLocked(old, storeElem, key)
}

func (t *btreeStore) getByKeyLocked(key string) (item interface{}, exists bool, err error) {
	t.tree.Ascend(func(i btree.Item) bool {
		if key == i.(*storeElement).Key {
			item = i
			exists = true
			return false
		}
		return true
	})

	return item, exists, nil
}

func (t *btreeStore) updateIndicesLocked(oldObj, newObj interface{}, key string) error {
	var oldIndexValues, indexValues []string
	var err error
	for name, indexFunc := range t.indexers {
		if oldObj != nil {
			oldIndexValues, err = indexFunc(oldObj)
		} else {
			oldIndexValues = oldIndexValues[:0]
		}
		if err != nil {
			return fmt.Errorf("unable to calculate an index entry for key %q on index %q: %v", key, name, err)
		}

		if newObj != nil {
			indexValues, err = indexFunc(newObj)
		} else {
			indexValues = indexValues[:0]
		}
		if err != nil {
			return fmt.Errorf("unable to calculate an index entry for key %q on index %q: %v", key, name, err)
		}

		index := t.indices[name]
		if index == nil {
			index = cache.Index{}
			t.indices[name] = index
		}

		if len(indexValues) == 1 && len(oldIndexValues) == 1 && indexValues[0] == oldIndexValues[0] {
			// We optimize for the most common case where indexFunc returns a single value which has not been changed
			continue
		}

		for _, value := range oldIndexValues {
			t.deleteKeyFromIndexLocked(key, value, index)
		}
		for _, value := range indexValues {
			t.addKeyToIndexLocked(key, value, index)
		}
	}

	return nil
}

func (c *btreeStore) addKeyToIndexLocked(key, value string, index cache.Index) {
	set := index[value]
	if set == nil {
		set = sets.String{}
		index[value] = set
	}
	set.Insert(key)
}

func (t *btreeStore) deleteKeyFromIndexLocked(key, value string, index cache.Index) {
	set := index[value]
	if set == nil {
		return
	}
	set.Delete(key)
	// If we don't delete the set when zero, indices with high cardinality
	// short lived resources can cause memory to increase over time from
	// unused empty sets. See `kubernetes/kubernetes/issues/84959`.
	if len(set) == 0 {
		delete(index, value)
	}
}

func (t *btreeStore) LimitPrefixRead(limit int64, key string) []interface{} {
	t.lock.RLock()
	defer t.lock.RUnlock()

	var result []interface{}
	var elementsRetrieved int64
	t.tree.AscendGreaterOrEqual(&storeElement{Key: key}, func(i btree.Item) bool {
		elementKey := i.(*storeElement).Key
		if limit > 0 && elementsRetrieved == limit {
			return false
		}
		if !strings.HasPrefix(elementKey, key) {
			return false
		}
		elementsRetrieved++
		result = append(result, i.(interface{}))
		return true
	})

	return result
}

func (t *btreeStore) ByIndex(indexName, indexValue string) ([]interface{}, error) {
	t.lock.RLock()
	defer t.lock.RUnlock()

	indexFunc := t.indexers[indexName]
	if indexFunc == nil {
		return nil, fmt.Errorf("Index with name %s does not exist", indexName)
	}

	index := t.indices[indexName]

	set := index[indexValue]
	list := make([]interface{}, 0, set.Len())
	for key := range set {
		obj, exists, err := t.getByKeyLocked(key)
		if err != nil {
			return nil, err
		}
		if !exists {
			return nil, fmt.Errorf("key %s does not exist in store", key)
		}
		list = append(list, obj)
	}

	return list, nil
}

var _ btreeIndexer = (*btreeStore)(nil)
