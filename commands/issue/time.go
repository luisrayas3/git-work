package issuecmd

import (
	"fmt"
	"time"

	"github.com/git-bug/git-bug/host"
)

// TimeFormsHelp is the TIME grammar, written once for every help text that
// names it: `--at` on get and the list, `--from` and `--to` on the log.
const TimeFormsHelp = "TIME is " + host.TimeForms

// parseTimeFlag reads one TIME flag, naming the flag when it refuses.
//
// An empty flag is the zero time, which is what the command means by "not
// given": the host reads that as now, or as no bound (doc/design/report.md).
func parseTimeFlag(name, value string) (time.Time, error) {
	t, err := host.ParseTime(value, time.Now())
	if err != nil {
		return time.Time{}, fmt.Errorf("%s: %w", name, err)
	}
	return t, nil
}
