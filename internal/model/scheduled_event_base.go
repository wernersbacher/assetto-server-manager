package model

import (
	"time"

	"github.com/teambition/rrule-go"
)

type ScheduledEventBase struct {
	Scheduled         time.Time
	ScheduledInitial  time.Time
	Recurrence        string
	ScheduledServerID ServerID
}

func (seb *ScheduledEventBase) SetRecurrenceRule(input string) error {
	rule, err := rrule.StrToRRule(input)
	if err != nil {
		return err
	}

	rule.DTStart(seb.ScheduledInitial)

	seb.Recurrence = rule.String()

	return nil
}

func (seb *ScheduledEventBase) GetRecurrenceRule() (*rrule.RRule, error) {
	rule, err := rrule.StrToRRule(seb.Recurrence)

	if err != nil {
		return nil, err
	}

	// dtstart is not saved in the string and must be reinitiated
	rule.DTStart(seb.ScheduledInitial)

	return rule, nil
}

func (seb *ScheduledEventBase) HasRecurrenceRule() bool {
	return seb.Recurrence != ""
}

func (seb *ScheduledEventBase) ClearRecurrenceRule() {
	seb.Recurrence = ""
}

func (seb *ScheduledEventBase) GetScheduledTime() time.Time {
	return seb.Scheduled
}
