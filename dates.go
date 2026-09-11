// German day and month names for parsing the DatumPlan field

package indiware2json

import "strings"

var dayReplacer = strings.NewReplacer(
	"Montag", "Monday",
	"Dienstag", "Tuesday",
	"Mittwoch", "Wednesday",
	"Donnerstag", "Thursday",
	"Freitag", "Friday",
	"Samstag", "Saturday",
	"Sonntag", "Sunday",
)

var monthReplacer = strings.NewReplacer(
	"Januar", "January",
	"Februar", "February",
	"März", "March",
	"April", "April",
	"Mai", "May",
	"Juni", "June",
	"Juli", "July",
	"August", "August",
	"September", "September",
	"Oktober", "October",
	"November", "November",
	"Dezember", "December",
)
