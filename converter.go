package indiware2json

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Convert automatically detects and converts the given XML document to JSON
func Convert(doc io.Reader) ([]byte, error) {
	data, err := io.ReadAll(doc)
	if err != nil {
		return nil, fmt.Errorf("failed to read doc: %w", err)
	}

	planType, err := getPlanType(data)
	if err != nil {
		return nil, fmt.Errorf("failed to get plan type: %w", err)
	}

	var plan []byte

	switch planType {
	case "K":
		classPlan, err := ConvertClasses(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}

		plan, err = json.Marshal(classPlan)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal class plan: %w", err)
		}
	case "L":
		teacherPlan, err := ConvertTeachers(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}

		plan, err = json.Marshal(teacherPlan)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal teacher plan: %w", err)
		}
	default:
		return nil, fmt.Errorf("failed to determine plan type, expected K or L in VpMobil > Kopf > planart")
	}

	return plan, nil
}

func getPlanType(doc []byte) (string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(doc))

	for {
		token, err := decoder.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}

			return "", fmt.Errorf("failed to walk xml document: %w", err)
		}

		switch t := token.(type) {

		case xml.StartElement:
			switch t.Name.Local {
			case "planart":
				var planType string

				err := decoder.DecodeElement(&planType, &t)
				if err != nil {
					return "", fmt.Errorf("failed to decode planart token: %w", err)
				}

				return planType, nil
			}
		}
	}

	return "", fmt.Errorf("reached EOF before planart element was found")
}

// ConvertRoomsJSON converts a class plan document to a JSON room plan
func ConvertRoomsJSON(doc io.Reader) ([]byte, error) {
	classPlan, err := ConvertClasses(doc)
	if err != nil {
		return nil, err
	}

	return json.Marshal(classPlan.Rooms())
}

// ConvertClasses converts a class plan document to a *ClassPlan
func ConvertClasses(doc io.Reader) (*ClassPlan, error) {
	var plan ClassPlan

	decoder := xml.NewDecoder(doc)

	var newClassEntry ClassEntry
	var schedules = make(map[string]Schedule, 1)

	for {
		token, err := decoder.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}

			return nil, fmt.Errorf("failed to walk xml document: %w", err)
		}

		switch t := token.(type) {

		case xml.StartElement:
			switch t.Name.Local {

			case "Kopf":
				meta, err := parseMeta(decoder)
				if err != nil {
					return nil, err
				}

				if meta.Type != "class" {
					return nil, fmt.Errorf("wrong plan type, expected class, got %s", meta.Type)
				}

				plan.Meta = *meta

			case "Kl":
				base, err := parseBase(decoder, schedules)
				if err != nil {
					return nil, err
				}

				newClassEntry = ClassEntry{
					BasePlanEntry: *base,
				}

			case "Kurse":
				var rawCourses struct {
					Courses []struct {
						Teacher string `xml:"KLe,attr"`
						Name    string `xml:",chardata"`
					} `xml:"Ku>KKz"`
				}

				err := decoder.DecodeElement(&rawCourses, &t)
				if err != nil {
					return nil, fmt.Errorf("failed to decode class Kurse token: %w", err)
				}

				newClassEntry.Courses = make([]CourseEntry, len(rawCourses.Courses))

				for i, c := range rawCourses.Courses {
					newClassEntry.Courses[i] = CourseEntry{Name: c.Name, Teacher: c.Teacher}
				}

			case "Unterricht":
				var rawUnits struct {
					Units []struct {
						Teacher string `xml:"UeLe,attr"`
						Subject string `xml:"UeFa,attr"`
						Group   string `xml:"UeGr,attr"`
						Number  string `xml:",chardata"`
					} `xml:"Ue>UeNr"`
				}

				err := decoder.DecodeElement(&rawUnits, &t)
				if err != nil {
					return nil, fmt.Errorf("failed to decode class Unterricht token: %w", err)
				}

				newClassEntry.Units = make([]UnitEntry, len(rawUnits.Units))

				for i, u := range rawUnits.Units {
					newUnitEntry := UnitEntry{
						Number:  u.Number,
						Teacher: u.Teacher,
						Subject: u.Subject,
					}

					if u.Group != "" {
						newUnitEntry.Group = &u.Group
					}

					newClassEntry.Units[i] = newUnitEntry
				}

			case "Pl":
				baseLessons, teachers, err := parsePlan(decoder, t)
				if err != nil {
					return nil, err
				}

				newClassEntry.Plan = make([]ClassLesson, len(baseLessons))

				for i := range baseLessons {
					classLesson := ClassLesson{BaseLesson: baseLessons[i], Teacher: teachers[i].Value}
					classLesson.Changes.Teacher = teachers[i].Changed

					newClassEntry.Plan[i] = classLesson
				}
			}

		case xml.EndElement:
			if t.Name.Local == "Kl" {
				plan.Classes = append(plan.Classes, newClassEntry)
			}
		}
	}

	plan.Schedules = make(map[string]Schedule, len(schedules))
	var scheduleMap = make(map[string]string, len(schedules))

	i := 1
	for n, s := range schedules {
		plan.Schedules[strconv.Itoa(i)] = s
		scheduleMap[n] = strconv.Itoa(i)
		i++
	}

	for i := range plan.Classes {
		plan.Classes[i].Schedule = scheduleMap[plan.Classes[i].Schedule]
	}

	return &plan, nil
}

// Rooms aggregates the class plan to a *RoomPlan
func (c *ClassPlan) Rooms() *RoomPlan {
	plan := RoomPlan{
		CreatedAt: c.CreatedAt,
		Date:      c.Date,
	}

	roomMap := make(map[string]*RoomEntry)

	for _, class := range c.Classes {
		for _, l := range class.Plan {
			if l.Room == nil {
				continue
			}

			r, ok := roomMap[*l.Room]
			if !ok {
				r = &RoomEntry{Code: *l.Room}
				roomMap[*l.Room] = r
			}

			newScheduleEntry := RoomScheduleEntry{
				Period:  l.Period,
				Start:   l.Start,
				End:     l.End,
				Class:   class.Name,
				Subject: l.Subject,
				Teacher: l.Teacher,
			}

			r.Schedule = append(r.Schedule, newScheduleEntry)
		}
	}

	plan.Rooms = make([]RoomEntry, len(roomMap))

	i := 0
	for _, r := range roomMap {
		sort.Slice(r.Schedule, func(i, j int) bool {
			return r.Schedule[i].Period < r.Schedule[j].Period
		})

		plan.Rooms[i] = *r

		i++
	}

	sort.Slice(plan.Rooms, func(i, j int) bool {
		return plan.Rooms[i].Code < plan.Rooms[j].Code
	})

	return &plan
}

// ConvertTeachers converts a teacher plan document to a *TeacherPlan
func ConvertTeachers(doc io.Reader) (*TeacherPlan, error) {
	var plan TeacherPlan

	var newTeacherEntry TeacherEntry
	var schedules = make(map[string]Schedule)

	decoder := xml.NewDecoder(doc)

	for {
		token, err := decoder.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}

			return nil, fmt.Errorf("failed to walk xml document: %w", err)
		}

		switch t := token.(type) {

		case xml.StartElement:
			switch t.Name.Local {

			case "Kopf":
				meta, err := parseMeta(decoder)
				if err != nil {
					return nil, err
				}

				if meta.Type != "teacher" {
					return nil, fmt.Errorf("wrong plan type, expected teacher, got %s", meta.Type)
				}

				plan.Meta = *meta

			case "Kl":
				base, err := parseBase(decoder, schedules)
				if err != nil {
					return nil, err
				}

				newTeacherEntry = TeacherEntry{
					BasePlanEntry: *base,
				}

			case "Pl":
				baseLessons, classes, err := parsePlan(decoder, t)
				if err != nil {
					return nil, err
				}

				newTeacherEntry.Plan = make([]TeacherLesson, len(baseLessons))

				for i := range baseLessons {
					newLesson := TeacherLesson{
						BaseLesson: baseLessons[i],
						Class:      classes[i].Value,
					}

					newLesson.Changes.Class = classes[i].Changed

					newTeacherEntry.Plan[i] = newLesson
				}

			case "Aufsichten":
				var supervision struct {
					Supervisions []struct {
						Subsitution  string `xml:"AuAe,attr"`
						Day          int    `xml:"AuTag"`
						BeforePeriod int    `xml:"AuVorStunde"`
						Time         string `xml:"AuUhrzeit"`
						Slot         string `xml:"AuZeit"`
						Location     string `xml:"AuOrt"`
						ForTeacher   string `xml:"AuFuer"`
						Note         string `xml:"AuInfo"`
					} `xml:"Aufsicht"`
				}

				err := decoder.DecodeElement(&supervision, &t)
				if err != nil {
					return nil, fmt.Errorf("failed to decode teacher Aufsichten token: %w", err)
				}

				newTeacherEntry.Supervision = make([]SupervisionEntry, len(supervision.Supervisions))

				for i, s := range supervision.Supervisions {
					newSupervisionEntry := SupervisionEntry{
						Day:          time.Weekday(s.Day % 7),
						BeforePeriod: s.BeforePeriod,
						Time:         s.Time,
						Slot:         s.Slot,
						Location:     s.Location,
					}

					if s.ForTeacher != "" {
						newSupervisionEntry.ForTeacher = &s.ForTeacher
					}
					if s.Note != "" {
						newSupervisionEntry.Note = &s.Note
					}

					switch s.Subsitution {
					case "AuVertretung":
						newSupervisionEntry.IsSubstituted = true
					case "AuAusfall":
						newSupervisionEntry.IsCancelled = true
					}

					newTeacherEntry.Supervision[i] = newSupervisionEntry
				}
			}

		case xml.EndElement:
			if t.Name.Local == "Kl" {
				plan.Teachers = append(plan.Teachers, newTeacherEntry)
			}
		}
	}

	plan.Schedules = make(map[string]Schedule, len(schedules))
	var scheduleMap = make(map[string]string, len(schedules))

	i := 1
	for n, s := range schedules {
		plan.Schedules[strconv.Itoa(i)] = s
		scheduleMap[n] = strconv.Itoa(i)
		i++
	}

	for i := range plan.Teachers {
		plan.Teachers[i].Schedule = scheduleMap[plan.Teachers[i].Schedule]
	}

	return &plan, nil
}

func parseBase(decoder *xml.Decoder, schedules map[string]Schedule) (*BasePlanEntry, error) {
	var base BasePlanEntry

	for {
		token, err := decoder.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}

			return nil, fmt.Errorf("failed to walk xml document: %w", err)
		}

		switch t := token.(type) {
		case xml.StartElement:
			switch t.Name.Local {

			case "Kurz":
				var name string
				err := decoder.DecodeElement(&name, &t)
				if err != nil {
					return nil, fmt.Errorf("failed to pass class Kurz token: %w", err)
				}

				base.Name = name

			case "Hash":
				var hash string
				err := decoder.DecodeElement(&hash, &t)
				if err != nil {
					return nil, fmt.Errorf("failed to decode class Hash token: %w", err)
				}

				if hash != "" {
					base.Hash = &hash
				}

			case "KlStunden":
				var rawSchedule struct {
					Periods []struct {
						From string `xml:"ZeitVon,attr"`
						To   string `xml:"ZeitBis,attr"`
						Num  int    `xml:",chardata"`
					} `xml:"KlSt"`
				}

				err := decoder.DecodeElement(&rawSchedule, &t)
				if err != nil {
					return nil, fmt.Errorf("failed to decode class KlStunden token: %w", err)
				}

				newSchedule := make([]Period, len(rawSchedule.Periods))
				var b strings.Builder
				for i, p := range rawSchedule.Periods {
					period := Period{
						Number: p.Num,
						Start:  p.From,
						End:    p.To,
					}

					newSchedule[i] = period

					b.WriteString((p.From + p.To + strconv.Itoa(p.Num)))
				}

				schedules[b.String()] = newSchedule

				base.Schedule = b.String()

				return &base, nil
			}
		}
	}

	return nil, fmt.Errorf("reached EOF before expected elements were found")
}

func parsePlan(decoder *xml.Decoder, t xml.StartElement) ([]BaseLesson, []struct {
	Value   *string
	Changed bool
}, error) {
	var rawLessons struct {
		Lessons []struct {
			Period  int    `xml:"St"`
			Start   string `xml:"Beginn"`
			End     string `xml:"Ende"`
			Subject struct {
				Changed string `xml:"FaAe,attr"`
				Value   string `xml:",chardata"`
			} `xml:"Fa"`
			Teacher struct {
				Changed string `xml:"LeAe,attr"`
				Value   string `xml:",chardata"`
			} `xml:"Le"`
			Room struct {
				Changed string `xml:"RaAe,attr"`
				Value   string `xml:",chardata"`
			} `xml:"Ra"`
			Course     string `xml:"Ku2"`
			UnitNumber string `xml:"Nr"`
			Note       string `xml:"If"`
		} `xml:"Std"`
	}

	err := decoder.DecodeElement(&rawLessons, &t)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to decode class Pl token: %w", err)
	}

	baseLessons := make([]BaseLesson, len(rawLessons.Lessons))
	entityValues := make([]struct {
		Value   *string
		Changed bool
	}, len(rawLessons.Lessons))

	for i, l := range rawLessons.Lessons {
		newLesson := BaseLesson{
			Period:     l.Period,
			Start:      l.Start,
			End:        l.End,
			UnitNumber: l.UnitNumber,
			Changes:    LessonChanges{},
		}

		var entityValue struct {
			Value   *string
			Changed bool
		}

		if l.Subject.Value != "" {
			newLesson.Subject = &l.Subject.Value
		}
		if l.Teacher.Value != "" {
			entityValue.Value = &l.Teacher.Value
		}
		if l.Room.Value != "" {
			newLesson.Room = &l.Room.Value
		}
		if l.Note != "" {
			newLesson.Note = &l.Note
		}
		if l.Course != "" {
			newLesson.Course = &l.Course
		}

		if l.Subject.Changed == "FaGeaendert" {
			newLesson.Changes.Subject = true
		}
		if l.Teacher.Changed == "LeGeaendert" {
			entityValue.Changed = true
		}
		if l.Room.Changed == "RaGeaendert" {
			newLesson.Changes.Room = true
		}

		baseLessons[i] = newLesson
		entityValues[i] = entityValue
	}

	return baseLessons, entityValues, nil
}

func parseMeta(decoder *xml.Decoder) (*Meta, error) {
	var meta Meta

	timeLocation, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		return nil, fmt.Errorf("for some reason loading the time location failed: %w", err)
	}

	for {
		token, err := decoder.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}

			return nil, fmt.Errorf("failed to walk xml document: %w", err)
		}

		switch t := token.(type) {
		case xml.StartElement:
			switch t.Name.Local {

			case "planart":
				var planart string
				err := decoder.DecodeElement(&planart, &t)
				if err != nil {
					return nil, fmt.Errorf("failed to decode planart token: %w", err)
				}

				switch planart {
				case "K":
					meta.Type = TypeClass
				case "L":
					meta.Type = TypeTeacher
				default:
					return nil, fmt.Errorf("Unrecognized plan type token: %s", planart)
				}
			case "zeitstempel":
				var rawCreationTime string
				err := decoder.DecodeElement(&rawCreationTime, &t)
				if err != nil {
					return nil, fmt.Errorf("failed to decode zeitstempel token: %w", err)
				}

				meta.CreatedAt, err = time.ParseInLocation("02.01.2006, 15:04", rawCreationTime, timeLocation)
				if err != nil {
					return nil, fmt.Errorf("failed to parse zeitstempel creation time: %w", err)
				}

			case "DatumPlan":
				var rawDate string
				err = decoder.DecodeElement(&rawDate, &t)
				if err != nil {
					return nil, fmt.Errorf("failed to decode DatumPlan token: %w", err)
				}

				rawDate = dayReplacer.Replace(rawDate)
				rawDate = monthReplacer.Replace(rawDate)

				meta.Date, err = time.ParseInLocation("Monday, 02. January 2006", rawDate, timeLocation)
				if err != nil {
					return nil, fmt.Errorf("failed to parse DatumPlan date time: %w", err)
				}

			case "datei":
				err = decoder.DecodeElement(&meta.Filename, &t)
				if err != nil {
					return nil, fmt.Errorf("failed to decode datei token: %w", err)
				}

			case "nativ":
				err = decoder.DecodeElement(&meta.Native, &t)
				if err != nil {
					return nil, fmt.Errorf("failed to decode nativ token: %w", err)
				}

			case "woche":
				err = decoder.DecodeElement(&meta.Week, &t)
				if err != nil {
					return nil, fmt.Errorf("failed to decode woche token: %w", err)
				}

			case "tageprowoche":
				err = decoder.DecodeElement(&meta.DaysPerWeek, &t)
				if err != nil {
					return nil, fmt.Errorf("failed to decode tageperwoche token: %w", err)
				}

			case "schulnummer":
				var rawSchoolNumber string
				err = decoder.DecodeElement(&rawSchoolNumber, &t)
				if err != nil {
					return nil, fmt.Errorf("failed to decode schulnummer token: %w", err)
				}

				if rawSchoolNumber != "" {
					meta.SchoolNumber = &rawSchoolNumber
				}

			case "FreieTage":
				var ft struct {
					Days []string `xml:"ft"`
				}

				err := decoder.DecodeElement(&ft, &t)
				if err != nil {
					return nil, fmt.Errorf("failed to decode FreieTage token: %w", err)
				}

				meta.FreeDates = make([]time.Time, len(ft.Days))

				for i, d := range ft.Days {
					parsedDate, err := time.ParseInLocation("060102", d, timeLocation)
					if err != nil {
						return nil, fmt.Errorf("failed to parse free day %s: %w", d, err)
					}

					meta.FreeDates[i] = parsedDate
				}

				return &meta, nil
			}
		}
	}

	return nil, fmt.Errorf("reached EOF before expected elements were found")
}
