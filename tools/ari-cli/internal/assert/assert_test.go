package assert

import "testing"

func TestMustPanicsOnError(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()

	Must(testErr("boom"))
}

func TestMust1ReturnsValue(t *testing.T) {
	v := Must1("ok", nil)
	if v != "ok" {
		t.Fatalf("unexpected value: %q", v)
	}
}

func TestInvariantPanicsWhenFalse(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic")
		}
	}()

	Invariant(false, "invariant failed")
}

type testErr string

func (e testErr) Error() string { return string(e) }
