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

	var planType struct {
		PlanArt string `xml:"Kopf>planart"`
	}

	err = xml.Unmarshal(data, &planType)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal xml document: %w", err)
	}

	var plan []byte

	switch planType.PlanArt {
	case "K":
		var classPlan *ClassPlan
		classPlan, err = ConvertClasses(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}

		plan, err = json.Marshal(classPlan)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal class plan: %w", err)
		}
	case "L":
		fmt.Println("Teacherplan")
	default:
		return nil, fmt.Errorf("failed to determine plan type, expected K or L in VpMobil > Kopf > planart")
	}

	return plan, nil
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
	plan := ClassPlan{}

	decoder := xml.NewDecoder(doc)

	timeLocation, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		return nil, fmt.Errorf("for some reason loading the time location failed: %w", err)
	}

	var inClass string
	var newClassEntry ClassEntry
	var schedules = make(map[string]Schedule)

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
				err = decoder.DecodeElement(&planart, &t)
				if err != nil {
					return nil, fmt.Errorf("failed to decode planart token: %w", err)
				}

				switch planart {
				case "K":
					plan.Type = TypeClass
				case "T":
					plan.Type = TypeTeacher
				default:
					return nil, fmt.Errorf("Unrecognized plan type token: %s", planart)
				}
			case "zeitstempel":
				var rawCreationTime string
				err = decoder.DecodeElement(&rawCreationTime, &t)
				if err != nil {
					return nil, fmt.Errorf("failed to decode zeitstempel token: %w", err)
				}

				plan.CreatedAt, err = time.ParseInLocation("02.01.2006, 15:04", rawCreationTime, timeLocation)
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

				plan.Date, err = time.Parse("Monday, 02. January 2006", rawDate)
				if err != nil {
					return nil, fmt.Errorf("failed to parse DatumPlan date time: %w", err)
				}

			case "datei":
				err = decoder.DecodeElement(&plan.Filename, &t)
				if err != nil {
					return nil, fmt.Errorf("failed to decode datei token: %w", err)
				}

			case "nativ":
				err = decoder.DecodeElement(&plan.Native, &t)
				if err != nil {
					return nil, fmt.Errorf("failed to decode nativ token: %w", err)
				}

			case "woche":
				err = decoder.DecodeElement(&plan.Week, &t)
				if err != nil {
					return nil, fmt.Errorf("failed to decode woche token: %w", err)
				}

			case "tageprowoche":
				err = decoder.DecodeElement(&plan.DaysPerWeek, &t)
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
					plan.SchoolNumber = &rawSchoolNumber
				}

			case "FreieTage":
				var ft struct {
					Days []string `xml:"ft"`
				}

				err := decoder.DecodeElement(&ft, &t)
				if err != nil {
					return nil, fmt.Errorf("failed to decode FreieTage token: %w", err)
				}

				for _, d := range ft.Days {
					parsed, err := time.ParseInLocation("060102", d, timeLocation)
					if err != nil {
						return nil, fmt.Errorf("failed to parse free day %s: %w", d, err)
					}

					plan.FreeDates = append(plan.FreeDates, parsed)
				}

			case "Kurz":
				err := decoder.DecodeElement(&inClass, &t)
				if err != nil {
					return nil, fmt.Errorf("failed to pass class Kurz token: %w", err)
				}

				newClassEntry = ClassEntry{
					Name: inClass,
				}

			case "Hash":
				var hash string
				err := decoder.DecodeElement(&hash, &t)
				if err != nil {
					return nil, fmt.Errorf("failed to decode class Hash token: %w", err)
				}

				if hash != "" {
					newClassEntry.Hash = &hash
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

				var newSchedule Schedule
				var b strings.Builder
				for _, p := range rawSchedule.Periods {
					period := Period{
						Number: p.Num,
						Start:  p.From,
						End:    p.To,
					}

					newSchedule = append(newSchedule, period)

					b.WriteString((p.From + p.To + strconv.Itoa(p.Num)))
				}

				schedules[b.String()] = newSchedule

				newClassEntry.Schedule = strconv.Itoa(len(schedules))

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

				for _, c := range rawCourses.Courses {
					newClassEntry.Courses = append(newClassEntry.Courses, CourseEntry{Name: c.Name, Teacher: c.Teacher})
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

				for _, u := range rawUnits.Units {
					newUnitEntry := UnitEntry{
						Number:  u.Number,
						Teacher: u.Teacher,
						Subject: u.Subject,
					}

					if u.Group != "" {
						newUnitEntry.Group = &u.Group
					}

					newClassEntry.Units = append(newClassEntry.Units, newUnitEntry)
				}

			case "Pl":
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
					return nil, fmt.Errorf("failed to decode class Pl token: %w", err)
				}

				for _, l := range rawLessons.Lessons {
					newLesson := Lesson{
						Period:     l.Period,
						Start:      l.Start,
						End:        l.End,
						UnitNumber: l.UnitNumber,
						Changes:    LessonChanges{},
					}

					if l.Subject.Value != "" {
						newLesson.Subject = &l.Subject.Value
					}
					if l.Teacher.Value != "" {
						newLesson.Teacher = &l.Teacher.Value
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
						newLesson.Changes.Teacher = true
					}
					if l.Room.Changed == "RaGeaendert" {
						newLesson.Changes.Room = true
					}

					newClassEntry.Plan = append(newClassEntry.Plan, newLesson)
				}
			}

		case xml.EndElement:
			if t.Name.Local == "Kl" {
				plan.Classes = append(plan.Classes, newClassEntry)
			}
		}
	}

	plan.Schedules = make(map[int]Schedule)

	i := 1
	for _, s := range schedules {
		plan.Schedules[i] = s
		i++
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

	for _, r := range roomMap {
		sort.Slice(r.Schedule, func(i, j int) bool {
			return r.Schedule[i].Period < r.Schedule[j].Period
		})

		plan.Rooms = append(plan.Rooms, *r)
	}

	sort.Slice(plan.Rooms, func(i, j int) bool {
		return plan.Rooms[i].Code < plan.Rooms[j].Code
	})

	return &plan
}
