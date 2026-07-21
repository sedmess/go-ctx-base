package values

import (
	"errors"
	"testing"
)

func TestCopyReturnsIndependentPointer(t *testing.T) {
	original := []int{1, 2}
	copyPointer := Copy(original)
	if copyPointer == nil || len(*copyPointer) != 2 {
		t.Fatalf("copy = %#v", copyPointer)
	}
	scalarCopy := Copy(42)
	if scalarCopy == nil || *scalarCopy != 42 {
		t.Fatalf("scalar copy = %#v", scalarCopy)
	}
}

func TestOptionalPresentContract(t *testing.T) {
	optional := IfError("value", nil)
	if !optional.HasValue() || optional.OrDefault("default") != "value" || optional.String() != "value" {
		t.Fatalf("present optional = has %v, default %q, string %q", optional.HasValue(), optional.OrDefault("default"), optional.String())
	}
}

func TestOptionalAbsentErrorContract(t *testing.T) {
	optional := IfError("discarded", errors.New("synthetic failure"))
	if optional.HasValue() || optional.OrDefault("default") != "default" || optional.String() != "<empty>" {
		t.Fatalf("absent optional = has %v, default %q, string %q", optional.HasValue(), optional.OrDefault("default"), optional.String())
	}
	zero := Optional[int]{}
	if zero.HasValue() || zero.OrDefault(7) != 7 || zero.String() != "<empty>" {
		t.Fatalf("zero optional = has %v, default %d, string %q", zero.HasValue(), zero.OrDefault(7), zero.String())
	}
}
