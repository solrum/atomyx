package session

import (
	"fmt"
	"strconv"
)

// formatBudget renders a budget in milliseconds as seconds for a
// message, keeping a fractional part so a sub-second budget is not
// reported as zero.
func formatBudget(ms int64) string {
	if ms == 1000 {
		return "1 second"
	}
	return fmt.Sprintf("%s seconds", strconv.FormatFloat(float64(ms)/1000, 'f', -1, 64))
}
