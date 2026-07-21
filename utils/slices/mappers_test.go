package slices

import (
	"reflect"
	"strconv"
	"testing"
)

func TestMapNilEmptyOrderAndCardinality(t *testing.T) {
	if mapped := Map[int, string](nil, strconv.Itoa); mapped != nil {
		t.Fatalf("nil Map result = %#v", mapped)
	}
	empty := Map([]int{}, strconv.Itoa)
	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty Map result = %#v", empty)
	}
	callCount := 0
	mapped := Map([]int{3, 1, 2}, func(value int) string {
		callCount++
		return strconv.Itoa(value)
	})
	if callCount != 3 || !reflect.DeepEqual(mapped, []string{"3", "1", "2"}) {
		t.Fatalf("Map calls/result = %d/%v", callCount, mapped)
	}
}

func TestFlatMapNilEmptyOrderAndCardinality(t *testing.T) {
	if mapped := FlatMap[int, int](nil, func(value int) []int { return []int{value} }); mapped != nil {
		t.Fatalf("nil FlatMap result = %#v", mapped)
	}
	empty := FlatMap([]int{}, func(value int) []int { return []int{value} })
	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty FlatMap result = %#v", empty)
	}
	mapped := FlatMap([]int{2, 1, 3}, func(value int) []int {
		if value == 1 {
			return nil
		}
		return []int{value, value * 10}
	})
	if !reflect.DeepEqual(mapped, []int{2, 20, 3, 30}) {
		t.Fatalf("FlatMap order/cardinality = %v", mapped)
	}
}

func TestToMapUniqueNilEmptyAndDuplicateContract(t *testing.T) {
	if mapped := ToMapUnique[int, int](nil, func(value int) int { return value }); mapped != nil {
		t.Fatalf("nil ToMapUnique result = %#v", mapped)
	}
	empty := ToMapUnique([]int{}, func(value int) int { return value })
	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty ToMapUnique result = %#v", empty)
	}
	mapped := ToMapUnique([]string{"first", "second", "third"}, func(value string) int { return len(value) })
	if len(mapped) != 2 || mapped[5] != "third" || mapped[6] != "second" {
		t.Fatalf("ToMapUnique duplicate/cardinality = %v", mapped)
	}
}
