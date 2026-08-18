package relay

import "fmt"

// BatchCeiling is the hard admission bound.
const BatchCeiling = 256

func AdmitBatch(size int) error {
	if size < 1 {
		return fmt.Errorf("batch size must be positive")
	}
	if size > BatchCeiling {
		return fmt.Errorf("batch exceeds ceiling %d", BatchCeiling)
	}
	return nil
}
