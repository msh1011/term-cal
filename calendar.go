package main

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/eidolon/wordwrap"
	"github.com/fatih/color"
	"google.golang.org/api/calendar/v3"
	"google.golang.org/api/option"
)

type CalendarRequest struct {
	UUID       string
	MaxResults int64  `schema:"limit"`
	TimeZone   string `schema:"tz"`
	AllDay     bool   `schema:"allDay"`
	Highlights string `schema:"highlights"`
	Exclude    string `schema:"exclude"`
	MaxWidth   int    `schema:"width"`
	NoColor    bool   `schema:"noColor"`

	colorMap map[*regexp.Regexp]string
	exclude  []*regexp.Regexp
}

func NewCalendarRequest(uuid string) *CalendarRequest {
	return &CalendarRequest{
		UUID:       uuid,
		MaxResults: 10,
		TimeZone:   "America/New_York",
		AllDay:     true,
		colorMap:   map[*regexp.Regexp]string{},
		exclude:    []*regexp.Regexp{},
	}
}

func (c *CalendarRequest) String() string {
	return fmt.Sprintf("(%s) max: %d", c.UUID, c.MaxResults)
}

func (c *CalendarRequest) Prepare() error {
	if c.UUID == "" {
		return fmt.Errorf("Invalid UUID")
	}
	if c.MaxResults > 50 {
		c.MaxResults = 50
	}
	if c.MaxWidth == 0 {
		c.MaxWidth = 50
	}
	if c.Exclude != "" {
		for _, v := range strings.Split(c.Exclude, ",") {
			r1, err := regexp.Compile(v)
			if err != nil {
				return err
			}
			c.exclude = append(c.exclude, r1)
		}
	}
	if c.Highlights != "" {
		h := strings.Split(c.Highlights, ",")
		if len(h)%2 != 0 {
			return fmt.Errorf("Invalid Highlights, (should be csv list of regex,color)")
		}
		for i := 0; i < len(h); i += 2 {
			r1, err := regexp.Compile(h[i])
			if err != nil {
				return err
			}
			c.colorMap[r1] = h[i+1]
		}
	}
	return nil
}

var colorMap = map[string]func(...interface{}) string{
	"black":   color.New(color.FgBlack).SprintFunc(),
	"red":     color.New(color.FgRed).SprintFunc(),
	"green":   color.New(color.FgGreen).SprintFunc(),
	"yellow":  color.New(color.FgYellow).SprintFunc(),
	"blue":    color.New(color.FgBlue).SprintFunc(),
	"magenta": color.New(color.FgMagenta).SprintFunc(),
	"cyan":    color.New(color.FgCyan).SprintFunc(),
	"white":   color.New(color.FgWhite).SprintFunc(),
}

func (c *CalendarRequest) ColorTitle(s, resp string) string {
	if c.NoColor {
		return s
	}
	for r, v := range c.colorMap {
		if _, ok := colorMap[v]; !ok {
			continue
		}
		if r.MatchString(s) {
			return colorMap[v](s)
		}
	}
	switch resp {
	case "accepted":
		return colorMap["green"](s)
	case "declined":
		return colorMap["red"](s)
	case "needsAction":
		fallthrough
	default:
		return s
	}
}

func (c *CalendarRequest) Generate() (string, error) {
	ctx := context.Background()

	user, err := userStore.Get(c.UUID)
	if err != nil {
		log.Printf("userStore get error (%s): %v", c.UUID, err)
		return "", fmt.Errorf("Error getting credentials")
	}

	srv, err := calendar.NewService(ctx, option.WithTokenSource(
		googleOauthConfig.TokenSource(ctx, &user.Token)))
	if err != nil {
		return "", fmt.Errorf("calendar.NewService failed with '%s'\n", err)
	}
	t := time.Now().Format(time.RFC3339)

	events, err := srv.Events.List("primary").ShowDeleted(false).
		SingleEvents(true).
		TimeMin(t).
		MaxResults(c.MaxResults).
		OrderBy("startTime").
		Do()
	if err != nil {
		return "", fmt.Errorf("Unable to retrieve next ten of the user's events: %v\n", err)
	}

	wrapper := wordwrap.Wrapper(c.MaxWidth, false)
	ret := ""
	if len(events.Items) == 0 {
		ret = "No upcoming events found."
	} else {
		for _, item := range events.Items {
			skip := false
			for _, v := range c.exclude {
				if v.MatchString(item.Summary) {
					skip = true
					break
				}
			}
			if skip {
				continue
			}
			l, err := time.LoadLocation(c.TimeZone)
			if err != nil {
				return "", fmt.Errorf("Invalid TZ (%s): %v", c.TimeZone, err)
			}
			today := time.Now().In(l)

			allDay := item.Start.DateTime == ""
			date := item.Start.DateTime
			inFmt := time.RFC3339
			outFmt := "Mon, Jan 02, 15:04"
			if allDay {
				date = item.Start.Date
				inFmt = "2006-01-02"
				outFmt = "Mon, Jan 02,"
				if !c.AllDay {
					continue
				}
			}

			t, err := time.ParseInLocation(inFmt, date, l)
			if err == nil {
				date = t.Format(outFmt)
			}

			timeU := time.Until(t).Round(time.Minute)
			days := int(timeU.Hours() / 24)
			until := ""

			if timeU < 0 {
				until = "Now"
			} else if sameDate(t, today) {
				until = shortHandUntil(timeU.Round(time.Minute * 1))
			} else if days > 0 {
				until = fmt.Sprintf("%dd", days)
				timeU -= time.Duration(days*24) * time.Hour
			} else {
				until = shortHandUntil(timeU)
			}
			resp := "needsAction"
			for _, v := range item.Attendees {
				if v.Email == user.ID {
					resp = v.ResponseStatus
				}
			}
			ret += fmt.Sprintf("%s\n%s %s\n\n",
				c.ColorTitle(wrapper(item.Summary), resp),
				c.Color("cyan", date),
				c.Color("blue", until))
		}
	}
	return ret, nil
}

func (c *CalendarRequest) Color(name, text string) string {
	if c.NoColor {
		return text
	}
	return colorMap[name](text)
}

func shortHandUntil(d time.Duration) string {
	if d.Hours() >= 1.0 {
		return fmt.Sprintf("%dh", int(d.Hours()))
	} else {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}

}

func sameDate(a, b time.Time) bool {
	y1, m1, d1 := a.Date()
	y2, m2, d2 := b.Date()
	if d1 != d2 {
		return false
	}
	if m1 != m2 {
		return false
	}
	if y1 != y2 {
		return false
	}
	return true
}
