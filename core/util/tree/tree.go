package tree

import "sort"

// Node 定义树节点的基本接口。
// 实现此接口的类型可以被构建为树形结构。
type Node[T any] interface {
	// Id 返回节点的唯一标识符
	Id() int64
	// ParentId 返回父节点的标识符
	ParentId() int64
	// SetChildren 设置节点的子节点
	SetChildren(children []T)
}

// Comparable 定义节点排序接口。
// 实现此接口的节点将在构建树时按照排序值排序。
type Comparable interface {
	// Compare 返回用于排序的值,值越小越靠前
	Compare() int
}

// Builder 树构建器,提供更灵活的树构建选项。
type Builder[T Node[T]] struct {
	nodes    []T
	parentId int64
	sortFunc func(i, j T) bool
}

// NewBuilder 创建一个新的树构建器。
func NewBuilder[T Node[T]](nodes []T) *Builder[T] {
	return &Builder[T]{
		nodes:    nodes,
		parentId: 0,
	}
}

// WithParentId 设置根节点的父ID。
func (b *Builder[T]) WithParentId(parentId int64) *Builder[T] {
	b.parentId = parentId
	return b
}

// WithSort 设置自定义排序函数。
func (b *Builder[T]) WithSort(sortFunc func(i, j T) bool) *Builder[T] {
	b.sortFunc = sortFunc
	return b
}

// WithComparable 启用基于 Comparable 接口的自动排序。
func (b *Builder[T]) WithComparable() *Builder[T] {
	b.sortFunc = func(i, j T) bool {
		a, aOk := any(i).(Comparable)
		b, bOk := any(j).(Comparable)
		if !aOk || !bOk {
			return false
		}
		return a.Compare() < b.Compare()
	}
	return b
}

// Build 构建树形结构并返回根节点列表。
func (b *Builder[T]) Build() []T {
	if len(b.nodes) == 0 {
		return []T{}
	}

	// 构建父节点到子节点的映射
	nodeMap := make(map[int64][]T, len(b.nodes))
	for _, node := range b.nodes {
		parentId := node.ParentId()
		nodeMap[parentId] = append(nodeMap[parentId], node)
	}

	// 如果设置了排序函数,对每个父节点的子节点进行排序
	if b.sortFunc != nil {
		for _, children := range nodeMap {
			sort.Slice(children, func(i, j int) bool {
				return b.sortFunc(children[i], children[j])
			})
		}
	}

	// 递归构建树
	return b.buildTree(nodeMap, b.parentId)
}

// buildTree 递归构建树形结构。
func (b *Builder[T]) buildTree(nodeMap map[int64][]T, parentId int64) []T {
	children, exists := nodeMap[parentId]
	if !exists {
		return []T{}
	}

	// 为每个子节点递归构建其子树
	for _, child := range children {
		childId := child.Id()
		child.SetChildren(b.buildTree(nodeMap, childId))
	}

	return children
}

// Build 快速构建树形结构的便捷函数。
// 等价于 NewBuilder(nodes).WithParentId(parentId).Build()
func Build[T Node[T]](nodes []T, parentId int64) []T {
	return NewBuilder(nodes).WithParentId(parentId).Build()
}

// BuildWithSort 构建树并使用自定义排序函数。
func BuildWithSort[T Node[T]](nodes []T, parentId int64, sortFunc func(i, j T) bool) []T {
	return NewBuilder(nodes).WithParentId(parentId).WithSort(sortFunc).Build()
}

// BuildWithComparable 构建树并使用 Comparable 接口自动排序。
func BuildWithComparable[T Node[T]](nodes []T, parentId int64) []T {
	return NewBuilder(nodes).WithParentId(parentId).WithComparable().Build()
}
