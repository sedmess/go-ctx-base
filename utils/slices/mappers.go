package slices

func Map[P any, Q any](source []P, mapper func(data P) Q) []Q {
	if source == nil {
		return nil
	}
	result := make([]Q, len(source))
	for i := range source {
		result[i] = mapper(source[i])
	}
	return result
}

func FlatMap[P any, Q any](source []P, mapper func(data P) []Q) []Q {
	if source == nil {
		return nil
	}
	result := make([]Q, 0)
	for i := range source {
		for _, q := range mapper(source[i]) {
			result = append(result, q)
		}
	}
	return result
}

func ToMapUnique[V any, K comparable](source []V, mapper func(data V) K) map[K]V {
	if source == nil {
		return nil
	}
	result := make(map[K]V)
	for _, value := range source {
		result[mapper(value)] = value
	}
	return result
}
