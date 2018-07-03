package possum

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const (
	StopAction  ScheduledAction = -1
	NoopAction  ScheduledAction = 0
	StartAction ScheduledAction = 1
)

const scheduleTag = "possum:schedule" // OfficeHours

func AllWeekdays() []time.Weekday {
	return []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday, time.Saturday, time.Sunday}
}

type ScheduledAction int8

func (s ScheduledAction) String() string {
	switch s {
	case StartAction:
		return "start"
	case StopAction:
		return "stop"
	default:
		return "noop"
	}
}

func NewSchedule(name string) *Schedule {
	return &Schedule{
		Name: name,
	}
}

type Schedules []*Schedule

func (s Schedules) Find(name string) *Schedule {
	for _, sch := range s {
		if sch.Name == name {
			return sch
		}
	}
	return nil
}

type Schedule struct {
	Name      string
	Locations []*time.Location
	Periods   []*Period
}

func (s *Schedule) AddPeriod(timezone string, period *Period) error {
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return err
	}
	s.Locations = append(s.Locations, loc)
	s.Periods = append(s.Periods, period)
	return nil
}

func (s *Schedule) Action(t time.Time, isRunning bool) ScheduledAction {
	for i, rule := range s.Periods {
		// convert t into the timeZone
		convertedTime := t.In(s.Locations[i])

		if rule.InPeriod(convertedTime) && !isRunning {
			return StartAction
		}
		if !rule.InPeriod(convertedTime) && isRunning {
			return StopAction
		}
	}
	return NoopAction
}

func (s *Schedule) MarshalJSON() ([]byte, error) {
	type Alias Schedule

	var locations []string

	for _, a := range s.Locations {
		locations = append(locations, a.String())
	}
	return json.Marshal(&struct {
		Name      string
		Locations []string
		Periods   []*Period
	}{
		Name:      s.Name,
		Locations: locations,
		Periods:   s.Periods,
	})
}

func (s *Schedule) UnmarshalJSON(b []byte) error {
	var tmp struct {
		Name    string
		Periods []*Period
	}
	if err := json.Unmarshal(b, &tmp); err != nil {
		return err
	}
	s.Name = tmp.Name
	s.Periods = tmp.Periods

	// we need to manually parse the
	var d map[string]interface{}
	if err := json.Unmarshal(b, &d); err != nil {
		return err
	}
	if locations, ok := d["Locations"].([]interface{}); ok {
		for _, l := range locations {
			if name, ok := l.(string); ok {
				loc, err := time.LoadLocation(name)
				if err != nil {
					return err
				}
				s.Locations = append(s.Locations, loc)
			}
		}
	}
	return nil
}

func NewPeriod(start, stop string, weekdays []time.Weekday) (*Period, error) {

	startTime, err := NewKitchenTime(start)
	if err != nil {
		return nil, fmt.Errorf("start time %s", err)
	}

	stopTime, err := NewKitchenTime(stop)
	if err != nil {
		return nil, fmt.Errorf("stop time %s", err)
	}

	// @todo validate that the start time is before the end time

	return &Period{
		StartTime: startTime,
		StopTime:  stopTime,
		Weekdays:  weekdays,
	}, nil
}

// Period rule can multiple conditions, note that all conditions must be true for the AWS Instance Scheduler to apply the appropriate Action
type Period struct {
	StartTime *KitchenTime   // The time, in HH:MM format, that the changes will start.
	StopTime  *KitchenTime   // The time, in HH:MM format, that the changes will stop.
	Weekdays  []time.Weekday // A list of weekdays that will allow this rule to trigger, if not set, it means all weekdays
}

func (r *Period) String() string {
	str := fmt.Sprintf("%s-%s", r.StartTime, r.StopTime)
	if len(r.Weekdays) > 0 {
		var days []string
		for _, weekday := range r.Weekdays {
			days = append(days, weekday.String())
		}
		str += fmt.Sprintf(" [%s]", strings.Join(days, ", "))
	}
	return str
}

func (r *Period) InPeriod(t time.Time) bool {

	if !r.inWeekday(t) {
		return false
	}

	if r.beforeStart(t) {
		return false
	}

	if r.afterStop(t) {
		return false
	}

	return true
}

func (r *Period) beforeStart(t time.Time) bool {
	if r.StartTime.Hour > t.Hour() {
		return true
	}

	if r.StopTime.Hour == t.Hour() && r.StartTime.Minute > t.Minute() {
		return true
	}

	return false
}

func (r *Period) afterStop(t time.Time) bool {
	if r.StopTime.Hour < t.Hour() {
		return true
	}
	if r.StopTime.Hour == t.Hour() && r.StopTime.Minute < t.Minute() {
		return true
	}
	return false

}

func (r *Period) inWeekday(t time.Time) bool {
	if len(r.Weekdays) == 0 {
		return true
	}
	for _, weekday := range r.Weekdays {
		if weekday == t.Weekday() {
			return true
		}
	}
	return false
}

func (r *Period) MarshalJSON() ([]byte, error) {
	var weeksdays []string
	for _, wd := range r.Weekdays {
		weeksdays = append(weeksdays, wd.String())
	}

	return json.Marshal(&struct {
		StartTime string
		StopTime  string
		Weekdays  []string
	}{
		StartTime: r.StartTime.String(),
		StopTime:  r.StopTime.String(),
		Weekdays:  weeksdays,
	})
}

func (r *Period) UnmarshalJSON(b []byte) error {
	var alias = struct {
		StartTime string
		StopTime  string
		Weekdays  []string
	}{}

	err := json.Unmarshal(b, &alias)
	if err != nil {
		return err
	}

	r.StartTime, err = NewKitchenTime(alias.StartTime)
	if err != nil {
		return err
	}
	r.StopTime, err = NewKitchenTime(alias.StopTime)
	if err != nil {
		return err
	}

	r.Weekdays = []time.Weekday{}
	for _, sday := range alias.Weekdays {
		for _, day := range AllWeekdays() {
			if day.String() == sday {
				r.Weekdays = append(r.Weekdays, day)
			}
		}
	}
	return nil
}

func NewKitchenTime(kitchenTime string) (*KitchenTime, error) {
	s := strings.Split(kitchenTime, ":")
	const errFormat = "wrong format for time, should be 13:45, not %s"
	if len(s) != 2 {
		return nil, fmt.Errorf(errFormat, kitchenTime)
	}

	hour, err := strconv.Atoi(s[0])
	if err != nil {
		return nil, fmt.Errorf(errFormat, kitchenTime)
	}

	minute, err := strconv.Atoi(s[1])
	if err != nil {
		return nil, fmt.Errorf(errFormat, kitchenTime)
	}

	return &KitchenTime{Hour: hour, Minute: minute}, nil
}

type KitchenTime struct {
	Hour   int
	Minute int
}

func (d *KitchenTime) String() string {
	return fmt.Sprintf("%02d:%02d", d.Hour, d.Minute)
}
