package tree

import (
	"encoding/json"
	"testing"
)

type TreeNode struct {
	ID       int64       `json:"id"`
	ParentID int64       `json:"parent_id"`
	Title    string      `json:"title"`
	Order    int         `json:"order"`
	Children []*TreeNode `json:"children,omitempty"`
}

func (t *TreeNode) Id() int64 {
	return t.ID
}

func (t *TreeNode) ParentId() int64 {
	return t.ParentID
}

func (t *TreeNode) Compare() int {
	return t.Order
}

func (t *TreeNode) SetChildren(children []*TreeNode) {
	t.Children = children
}

func TestBuild(t *testing.T) {
	data := []*TreeNode{
		{ID: 1, ParentID: 0, Title: "1", Order: 2},
		{ID: 2, ParentID: 1, Title: "1.1", Order: 4},
		{ID: 3, ParentID: 1, Title: "1.2", Order: 3},
		{ID: 4, ParentID: 0, Title: "2", Order: 1},
		{ID: 5, ParentID: 4, Title: "2.1", Order: 0},
		{ID: 6, ParentID: 5, Title: "2.1.1", Order: 0},
	}

	tree := Build(data, 0)
	bytes, err := json.MarshalIndent(tree, "", "  ")
	if err != nil {
		t.Error(err)
	}

	t.Log(string(bytes))

	// 验证树结构
	if len(tree) != 2 {
		t.Errorf("Expected 2 root nodes, got %d", len(tree))
	}
}

func TestBuildWithComparable(t *testing.T) {
	data := []*TreeNode{
		{ID: 1, ParentID: 0, Title: "1", Order: 2},
		{ID: 2, ParentID: 1, Title: "1.1", Order: 4},
		{ID: 3, ParentID: 1, Title: "1.2", Order: 3},
		{ID: 4, ParentID: 0, Title: "2", Order: 1},
		{ID: 5, ParentID: 4, Title: "2.1", Order: 0},
		{ID: 6, ParentID: 5, Title: "2.1.1", Order: 0},
	}

	tree := BuildWithComparable(data, 0)
	bytes, err := json.MarshalIndent(tree, "", "  ")
	if err != nil {
		t.Error(err)
	}

	t.Log(string(bytes))

	// 验证排序 - 第一个节点应该是 Order=1 的节点
	if len(tree) < 2 {
		t.Fatal("Expected at least 2 root nodes")
	}
	if tree[0].Order != 1 {
		t.Errorf("Expected first node to have Order=1, got %d", tree[0].Order)
	}
	if tree[1].Order != 2 {
		t.Errorf("Expected second node to have Order=2, got %d", tree[1].Order)
	}

	// 验证子节点排序
	if len(tree[1].Children) >= 2 {
		if tree[1].Children[0].Order > tree[1].Children[1].Order {
			t.Errorf("Children not sorted correctly")
		}
	}
}

func TestBuildWithSort(t *testing.T) {
	data := []*TreeNode{
		{ID: 1, ParentID: 0, Title: "B", Order: 2},
		{ID: 2, ParentID: 0, Title: "A", Order: 1},
		{ID: 3, ParentID: 0, Title: "C", Order: 3},
	}

	// 按标题排序
	tree := BuildWithSort(data, 0, func(i, j *TreeNode) bool {
		return i.Title < j.Title
	})

	if len(tree) != 3 {
		t.Fatalf("Expected 3 nodes, got %d", len(tree))
	}

	if tree[0].Title != "A" || tree[1].Title != "B" || tree[2].Title != "C" {
		t.Errorf("Nodes not sorted by title correctly: %s, %s, %s", tree[0].Title, tree[1].Title, tree[2].Title)
	}
}

func TestNewBuilder(t *testing.T) {
	data := []*TreeNode{
		{ID: 1, ParentID: 0, Title: "1", Order: 2},
		{ID: 2, ParentID: 1, Title: "1.1", Order: 4},
		{ID: 3, ParentID: 1, Title: "1.2", Order: 3},
		{ID: 4, ParentID: 0, Title: "2", Order: 1},
	}

	// 使用 Builder 模式
	tree := NewBuilder(data).
		WithParentId(0).
		WithComparable().
		Build()

	bytes, err := json.MarshalIndent(tree, "", "  ")
	if err != nil {
		t.Error(err)
	}

	t.Log(string(bytes))

	// 验证排序
	if len(tree) >= 2 {
		if tree[0].Order > tree[1].Order {
			t.Errorf("Root nodes not sorted correctly")
		}
	}
}

func TestBuildEmptyNodes(t *testing.T) {
	var data []*TreeNode

	tree := Build(data, 0)
	if len(tree) != 0 {
		t.Errorf("Expected empty tree, got %d nodes", len(tree))
	}
}

func TestBuildDifferentParentID(t *testing.T) {
	data := []*TreeNode{
		{ID: 1, ParentID: 10, Title: "1", Order: 1},
		{ID: 2, ParentID: 1, Title: "1.1", Order: 1},
		{ID: 3, ParentID: 10, Title: "2", Order: 2},
	}

	// 构建从 ParentId=10 开始的树
	tree := Build(data, 10)

	if len(tree) != 2 {
		t.Errorf("Expected 2 root nodes, got %d", len(tree))
	}

	// 验证第一个节点有子节点
	if len(tree[0].Children) != 1 {
		t.Errorf("Expected first node to have 1 child, got %d", len(tree[0].Children))
	}
}
