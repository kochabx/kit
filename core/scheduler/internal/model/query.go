package model

type Scan struct {
	Cursor uint64
	Count  int64
}

type Page[T any] struct {
	Items      []T
	NextCursor uint64
}

type DeadQuery struct {
	Cursor   uint64
	Count    int64
	TaskName string
}
