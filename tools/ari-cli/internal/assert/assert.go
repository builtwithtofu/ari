package assert

import "fmt"

func Must(err error) {
	if err != nil {
		panic(err)
	}
}

func Must1[T any](value T, err error) T {
	if err != nil {
		panic(err)
	}

	return value
}

func Invariant(condition bool, format string, args ...any) {
	if condition {
		return
	}

	panic(fmt.Sprintf(format, args...))
}
