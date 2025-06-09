package values

import "fmt"

func Copy[T any](value T) *T {
	var cp T
	cp = value
	return &cp
}

type Optional[T any] struct {
	hasValue bool
	value    T
}

func (o Optional[T]) OrDefault(defValue T) T {
	if o.hasValue {
		return o.value
	} else {
	}
	return defValue
}

func (o Optional[T]) String() string {
	if o.hasValue {
		return fmt.Sprint(o.value)
	} else {
		return "<empty>"
	}
}

func (o Optional[T]) HasValue() bool {
	return o.hasValue
}

func IfError[T any](value T, err error) Optional[T] {
	if err != nil {
		return Optional[T]{hasValue: false}
	} else {
		return Optional[T]{hasValue: true, value: value}
	}
}
