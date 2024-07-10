package irdata

import (
	"fmt"
)

func makeErrorf(format string, a ...any) error {
	return fmt.Errorf("irdata: %s", fmt.Sprintf(format, a...))
}
