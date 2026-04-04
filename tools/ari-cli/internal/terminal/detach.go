package terminal

const defaultDetachByte = byte(0x1c)

type DetachScanner struct {
	matcher func(b byte) bool
}

func NewDetachScanner() *DetachScanner {
	return NewDetachScannerWithMatcher(func(b byte) bool {
		return b == defaultDetachByte
	})
}

func NewDetachScannerWithMatcher(matcher func(b byte) bool) *DetachScanner {
	if matcher == nil {
		matcher = func(b byte) bool {
			return b == defaultDetachByte
		}
	}

	return &DetachScanner{matcher: matcher}
}

func (s *DetachScanner) Scan(input []byte) ([]byte, bool) {
	if s == nil || s.matcher == nil {
		return append([]byte(nil), input...), false
	}

	for idx, b := range input {
		if s.matcher(b) {
			return append([]byte(nil), input[:idx]...), true
		}
	}

	return append([]byte(nil), input...), false
}
