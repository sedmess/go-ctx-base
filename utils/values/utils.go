package values

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

func IfError[T any](value T, err error) Optional[T] {
	if err != nil {
		return Optional[T]{hasValue: false}
	} else {
		return Optional[T]{hasValue: true, value: value}
	}
}
