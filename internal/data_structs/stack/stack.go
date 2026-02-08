package stack

type Stack[T any] []T

func (s *Stack[T]) Push(item T) {
	*s = append(*s, item)
}

func (s *Stack[T]) Pop() T {
	topIdx := len(*s) - 1
	item := (*s)[topIdx]
	*s = (*s)[:topIdx]
	return item
}
