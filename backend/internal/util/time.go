package util

import "time"

const TimeLayout = time.RFC3339

func NowUTC() time.Time {
	return time.Now().UTC()
}

func FormatTime(t time.Time) string {
	return t.UTC().Format(TimeLayout)
}

func ParseTime(value string) (time.Time, error) {
	return time.Parse(TimeLayout, value)
}
