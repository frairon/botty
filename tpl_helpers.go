package botty

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/dustin/go-humanize"
)

func RunTemplate(tpl string, values ...*KV) (string, error) {
	valueMap := make(map[string]interface{}, len(values))

	for _, value := range values {
		valueMap[value.Key] = value.Value
	}

	content := template.Must(template.New("").Funcs(templateFuncs).Parse(tpl))

	var buf bytes.Buffer
	err := content.Execute(&buf, valueMap)
	return buf.String(), err
}

var templateFuncs = template.FuncMap{
	"idx2selector":         idxToSelector,
	"selector2Idx":         selectorToIdx,
	"name2command":         nameToCommand,
	"formatUpdateTime":     formatUpdateTime,
	"formatUpdatedRelTime": formatUpdatedRelTime,
	"formatOnOff":          formatOnOff,
	"formatTimeHourMinute": formatTimeHourMinute,
	"divider":              func() string { return "========" },
}

type KV struct {
	Key   string
	Value interface{}
}

func kv(key string, value interface{}) *KV {
	return &KV{
		Key:   key,
		Value: value,
	}
}

// selectors = "abcdefghijklmnopqrstuvwxyz"
var selectors = []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12", "13", "14", "15", "16", "17", "18", "19", "20"}

func idxToSelector(idx int) string {
	if idx >= len(selectors) {
		return "-"
	}
	return string(selectors[idx])
}

func selectorToIdx(alpha string) int {
	for idx, selector := range selectors {
		if selector == alpha {
			return idx
		}
	}
	return -1
	// return strings.Index(selectors, alpha)
}

var cmdChars = regexp.MustCompile("[^a-zA-Z0-9_]+")

func truncateRunes(value string, max int) string {
	var cnt int
	for i := range value {
		cnt++

		if cnt >= max {
			return value[:i]
		}
	}
	return value
}

func nameToCommand(name string) string {
	name = truncateRunes(name, 50)
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, "ä", "ae")
	name = strings.ReplaceAll(name, "ö", "oe")
	name = strings.ReplaceAll(name, "ü", "ue")
	name = strings.ReplaceAll(name, "ß", "ss")
	return cmdChars.ReplaceAllString(name, "")
}

func formatUpdateTime(updTime time.Time) string {
	return updTime.Local().Format("Mon, 02 Jan 2006 15:04:05")
}

func formatUpdatedRelTime(updTime time.Time) string {
	return humanize.Time(updTime)
}

func formatTimeHourMinute(updTime time.Time) string {
	if updTime.IsZero() {
		return "never"
	}

	diff := time.Since(updTime)
	var prefix, suffix string
	if diff < 0 {
		prefix = "in "
		diff = -diff
	} else {
		suffix = " ago"
	}

	return fmt.Sprintf("%s%v%s", prefix, diff.Truncate(time.Second), suffix)
}

func formatOnOff(value bool) string {
	if value {
		return "ON"
	}
	return "OFF"
}
