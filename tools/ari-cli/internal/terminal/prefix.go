package terminal

// DefaultDetachPrefix is the default terminal prefix byte for detach behavior.
const DefaultDetachPrefix = defaultDetachByte

// PrefixScanner applies terminal prefix behavior to incoming input bytes.
type PrefixScanner struct {
	detach *DetachScanner
}

// NewPrefixScanner returns a scanner that detects the default detach prefix.
func NewPrefixScanner() *PrefixScanner {
	return &PrefixScanner{detach: NewDetachScanner()}
}

// Scan returns passthrough bytes and whether the detach prefix was seen.
func (s *PrefixScanner) Scan(input []byte) ([]byte, bool) {
	if s == nil || s.detach == nil {
		return append([]byte(nil), input...), false
	}

	return s.detach.Scan(input)
}
