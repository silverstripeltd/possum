package possum

import (
	"encoding/json"
	"testing"
	"time"
)

func TestSchedulingShouldStart(t *testing.T) {

	weekdays, err := NewPeriod("8:00", "18:00", []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday})
	if err != nil {
		t.Error(err)
		return
	}

	hoursOnly, err := NewPeriod("8:00", "18:00", nil)
	if err != nil {
		t.Error(err)
		return
	}

	tests := []struct {
		rule      *Period
		checkTime time.Time
		expected  bool
	}{
		{rule: weekdays, checkTime: newWeekday(time.Friday, 7, 59), expected: false},
		{rule: weekdays, checkTime: newWeekday(time.Monday, 8, 0), expected: true},
		{rule: weekdays, checkTime: newWeekday(time.Friday, 18, 1), expected: false},
		{rule: weekdays, checkTime: newWeekday(time.Saturday, 7, 59), expected: false},
		{rule: hoursOnly, checkTime: newWeekday(time.Friday, 7, 59), expected: false},
		{rule: hoursOnly, checkTime: newWeekday(time.Friday, 8, 0), expected: true},
		{rule: hoursOnly, checkTime: newWeekday(time.Saturday, 7, 59), expected: false},
		{rule: hoursOnly, checkTime: newWeekday(time.Saturday, 8, 0), expected: true},

		{rule: hoursOnly, checkTime: newWeekday(time.Monday, 7, 59), expected: false},
		{rule: hoursOnly, checkTime: newWeekday(time.Monday, 8, 0), expected: true},
		{rule: hoursOnly, checkTime: newWeekday(time.Monday, 8, 30), expected: true},
		{rule: hoursOnly, checkTime: newWeekday(time.Monday, 9, 0), expected: true},
		{rule: hoursOnly, checkTime: newWeekday(time.Monday, 9, 30), expected: true},

		{rule: hoursOnly, checkTime: newWeekday(time.Monday, 17, 59), expected: true},
		{rule: hoursOnly, checkTime: newWeekday(time.Monday, 18, 0), expected: true},
		{rule: hoursOnly, checkTime: newWeekday(time.Monday, 18, 30), expected: false},
		{rule: hoursOnly, checkTime: newWeekday(time.Monday, 19, 0), expected: false},
		{rule: hoursOnly, checkTime: newWeekday(time.Monday, 19, 30), expected: false},
		{rule: hoursOnly, checkTime: newWeekday(time.Monday, 20, 0), expected: false},
		{rule: hoursOnly, checkTime: newWeekday(time.Monday, 20, 30), expected: false},
	}

	for i, test := range tests {
		actual := test.rule.InPeriod(test.checkTime)
		if actual != test.expected {
			t.Errorf("%d, expected %t, but got %t for %02d:%02d %s\n", i+1, test.expected, actual, test.checkTime.Hour(), test.checkTime.Minute(), test.checkTime.Weekday())
		}
	}
}

func TestSchedule_getAction(t *testing.T) {

	dailyP, _ := NewPeriod("8:00", "17:00", nil)
	daily := NewSchedule("Daily")
	daily.AddPeriod(time.Local.String(), dailyP)

	officeP, _ := NewPeriod("9:00", "14:00", []time.Weekday{time.Monday, time.Tuesday, time.Wednesday, time.Thursday, time.Friday})
	office := NewSchedule("OfficeHours")
	office.AddPeriod("Pacific/Auckland", officeP)

	tests := []struct {
		s         *Schedule
		t         time.Time
		expected  ScheduledAction
		isRunning bool
	}{
		{s: daily, t: newWeekday(time.Friday, 7, 0), isRunning: false, expected: NoopAction},
		{s: daily, t: newWeekday(time.Friday, 12, 0), isRunning: false, expected: StartAction},
		{s: daily, t: newWeekday(time.Friday, 19, 0), isRunning: false, expected: NoopAction},
		{s: daily, t: newWeekday(time.Friday, 7, 0), isRunning: true, expected: StopAction},
		{s: daily, t: newWeekday(time.Friday, 12, 0), isRunning: true, expected: NoopAction},
		{s: daily, t: newWeekday(time.Friday, 19, 0), isRunning: true, expected: StopAction},
		{s: office, t: newWeekday(time.Friday, 7, 0), isRunning: true, expected: StopAction},
		{s: office, t: newWeekday(time.Friday, 12, 0), isRunning: true, expected: NoopAction},
		{s: office, t: newWeekday(time.Friday, 19, 0), isRunning: true, expected: StopAction},
		{s: office, t: newWeekday(time.Saturday, 12, 0), isRunning: true, expected: StopAction},
		{s: office, t: newWeekday(time.Saturday, 12, 0), isRunning: false, expected: NoopAction},
		{s: office, t: newWeekday(time.Monday, 9, 0), isRunning: false, expected: StartAction},
	}

	for i, test := range tests {
		actual := test.s.Action(test.t, test.isRunning)
		if actual != test.expected {
			t.Errorf("case %d. expected %s, but got %s for %02d:%02d %s, isRunning: %t\n", i+1, test.expected, actual, test.t.Hour(), test.t.Minute(), test.t.Weekday(), test.isRunning)
			for _, rule := range test.s.Periods {
				t.Errorf("\t%s\n", rule)
			}
		}
	}
}

func TestPeriod_JSONMarshalling(t *testing.T) {
	orig, err := NewPeriod("8:00", "9:00", []time.Weekday{time.Monday, time.Saturday})
	if err != nil {
		t.Error(err)
		return
	}

	actual, err := json.Marshal(orig)
	if err != nil {
		t.Error(err)
		return
	}

	expected := `{"StartTime":"08:00","StopTime":"09:00","Weekdays":["Monday","Saturday"]}`
	if string(actual) != expected {
		t.Errorf("Expected: %s\n Got: %s", expected, actual)
		return
	}

	var np Period
	if err := json.Unmarshal(actual, &np); err != nil {
		t.Error(err)
		return
	}

	if np.StartTime == nil {
		t.Errorf("Didn't expect that StartTime would be a nil pointer")
		return
	}

	if orig.StartTime.String() != np.StartTime.String() {
		t.Errorf("Expected StartTime '%s', got '%s'", orig.StartTime, np.StartTime)
	}

	if np.StopTime == nil {
		t.Errorf("Didn't expect that StopTime would be a nil pointer")
		return
	}

	if orig.StopTime.String() != np.StopTime.String() {
		t.Errorf("Expected Stoptime '%s', got '%s'", orig.StopTime, np.StopTime)
	}

	if 2 != len(np.Weekdays) {
		t.Errorf("Expected the %d weekdays, got %d", len(orig.Weekdays), len(np.Weekdays))
		return
	}

	for i := range orig.Weekdays {
		if orig.Weekdays[i] != np.Weekdays[i] {
			t.Errorf("Expected day %d to be %s, got %s", i, orig.Weekdays[i], np.Weekdays[i])
		}
	}

}

func newWeekday(weekday time.Weekday, hour, min int) time.Time {
	// 2015-5-6 is a sunday
	return time.Date(2018, 5, 6+int(weekday), hour, min, 0, 0, time.Local)
}
