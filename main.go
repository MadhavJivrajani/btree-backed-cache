package main

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func main() {
	tree := NewBTreeStore(2)
	for i := 1; i < 100; i++ {
		elem := storeElement{
			Rv:  uint64(i),
			Key: fmt.Sprintf("default/pod%d", i),
			Object: &v1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name: fmt.Sprintf("pod%d", i),
				},
			},
		}
		tree.Add(elem)
	}
	items := tree.ListSince(20)
	// Should print [20, 99].
	for i := 0; i < len(items); i++ {
		fmt.Println(items[i].(storeElement).Rv)
	}
	fmt.Println(tree.GetByKey("default/pod20"))
	fmt.Println(len(tree.List())) // 99
}
