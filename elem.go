package main

import (
	"github.com/google/btree"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
)

type storeElement struct {
	Rv     uint64
	Key    string
	Object runtime.Object
	Labels labels.Set
	Fields fields.Set
}

func (t storeElement) Less(than btree.Item) bool {
	return t.Rv < than.(storeElement).Rv
}

var _ btree.Item = storeElement{}
