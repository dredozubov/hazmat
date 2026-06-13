package hazmat

import (
	"bytes"
	"fmt"
)

func validateSeatbeltWrapperFile(read func(string) ([]byte, error), isExecutable func(string) (bool, error)) error {
	executable, err := isExecutable(seatbeltWrapperPath)
	if err != nil {
		return fmt.Errorf("inspect seatbelt wrapper executable bit as agent: %w", err)
	}
	if !executable {
		return fmt.Errorf("seatbelt wrapper is missing or not executable: %s", seatbeltWrapperPath)
	}
	data, err := read(seatbeltWrapperPath)
	if err != nil {
		return fmt.Errorf("read seatbelt wrapper as agent: %w", err)
	}
	if !bytes.Equal(data, []byte(seatbeltWrapperContent)) {
		return fmt.Errorf("seatbelt wrapper content drifted from Hazmat-managed template")
	}
	return nil
}
