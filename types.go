package indiware2json

import "time"

type Meta struct {
	Type         PlanType    `json:"type"`
	CreatedAt    time.Time   `json:"created_at"`
	Date         time.Time   `json:"date"`
	Week         int         `json:"week"`
	DaysPerWeek  int         `json:"days_per_week"`
	Filename     string      `json:"filename"`
	Native       int         `json:"native"`
	SchoolNumber *int        `json:"school_number"` // I don't actually know the data type here
	FreeDates    []time.Time `json:"free_dates"`
}

type PlanType string

const (
	TypeClass   PlanType = "class"
	TypeTeacher PlanType = "teacher"
)

type Period struct {
	Number int       `json:"period"`
	Start  time.Time `json:"start"`
	End    time.Time `json:"end"`
}

type Schedule []Period

type CourseEntry struct {
	Name    string `json:"name"`
	Teacher string `json:"teacher"`
}

type UnitEntry struct {
	Number  int     `json:"number"`
	Teacher string  `json:"teacher"`
	Subject string  `json:"subject"`
	Group   *string `json:"group"`
}

type Lesson struct {
	Period     int           `json:"period"`
	Start      time.Time     `json:"start"`
	End        time.Time     `json:"end"`
	Subject    *string       `json:"subject"`
	Course     *string       `json:"course"`
	Teacher    *string       `json:"teacher"`
	Room       *string       `json:"room"`
	UnitNumber int           `json:"unit_number"`
	Note       *string       `json:"note"`
	Changes    LessonChanges `json:"changes"`
}

type LessonChanges struct {
	Subject bool `json:"subject,omitempty"`
	Teacher bool `json:"teacher,omitempty"`
	Room    bool `json:"room,omitempty"`
}

type ClassPlan struct {
	Meta
	Schedules map[int]Schedule `json:"schedules"`
	Classes   []ClassEntry     `json:"classes"`
}

/* type TeacherPlan struct {
	Meta
	Schedules map[int]Schedule
	Teachers []TeacherEntry `json:"teachers"`
} */

type ClassEntry struct {
	Name     string        `json:"name"`
	Hash     *string       `json:"hash"`
	Schedule int           `json:"schedule"`
	Courses  []CourseEntry `json:"courses"`
	Units    []UnitEntry   `json:"units"`
	Plan     []Lesson      `json:"plan"`
}
