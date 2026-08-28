package scheduler

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Cron represents a standard five-field cron expression: minute, hour,
// day-of-month, month, and day-of-week.
type Cron struct {
	fields             [5]map[int]bool
	dayOfMonthWildcard bool
	dayOfWeekWildcard  bool
}

func ParseCron(expression string) (Cron, error) {
	parts := strings.Fields(expression)
	if len(parts) != 5 {
		return Cron{}, fmt.Errorf("cron must have five fields")
	}
	limits := [5][2]int{{0, 59}, {0, 23}, {1, 31}, {1, 12}, {0, 6}}
	var cron Cron
	for i, part := range parts {
		values, err := parseField(part, limits[i][0], limits[i][1])
		if err != nil {
			return Cron{}, fmt.Errorf("field %d: %w", i+1, err)
		}
		cron.fields[i] = values
	}
	cron.dayOfMonthWildcard = wildcardField(parts[2])
	cron.dayOfWeekWildcard = wildcardField(parts[4])
	return cron, nil
}

func wildcardField(field string) bool {
	for _, part := range strings.Split(field, ",") {
		if strings.HasPrefix(part, "*") {
			return true
		}
	}
	return false
}

func (c Cron) Matches(t time.Time) bool {
	if !c.fields[0][t.Minute()] || !c.fields[1][t.Hour()] || !c.fields[3][int(t.Month())] {
		return false
	}
	dom := c.fields[2][t.Day()]
	dow := c.fields[4][int(t.Weekday())]
	// Standard cron treats two restricted day fields as an OR.
	if !c.dayOfMonthWildcard && !c.dayOfWeekWildcard {
		return dom || dow
	}
	return dom && dow
}

func parseField(field string, min, max int) (map[int]bool, error) {
	values := make(map[int]bool)
	for _, part := range strings.Split(field, ",") {
		step := 1
		if strings.Contains(part, "/") {
			pieces := strings.Split(part, "/")
			if len(pieces) != 2 {
				return nil, fmt.Errorf("invalid step %q", part)
			}
			var err error
			step, err = strconv.Atoi(pieces[1])
			if err != nil || step <= 0 {
				return nil, fmt.Errorf("invalid step %q", part)
			}
			part = pieces[0]
		}

		start, end := min, max
		if part != "*" {
			if strings.Contains(part, "-") {
				pieces := strings.Split(part, "-")
				if len(pieces) != 2 {
					return nil, fmt.Errorf("invalid range %q", part)
				}
				var err error
				start, err = strconv.Atoi(pieces[0])
				if err != nil {
					return nil, fmt.Errorf("invalid value %q", part)
				}
				end, err = strconv.Atoi(pieces[1])
				if err != nil {
					return nil, fmt.Errorf("invalid value %q", part)
				}
			} else {
				var err error
				start, err = strconv.Atoi(part)
				if err != nil {
					return nil, fmt.Errorf("invalid value %q", part)
				}
				end = start
			}
		}
		if start < min || end > max || start > end {
			return nil, fmt.Errorf("value out of range in %q", part)
		}
		for value := start; value <= end; value += step {
			values[value] = true
		}
	}
	return values, nil
}
