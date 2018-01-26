package result

import (
	"bytes"
	"fmt"
	"html/template"
	"os"
)

type Result struct {
	Exit  string
	Error string
	host  string
}

func New() Result {
	return Result{}
}

func (r *Result) SetExit(ex string) {
	r.Exit = ex
	err := os.Getenv(fmt.Sprintf("EXIT_%s", ex))
	if err != "" {
		r.Error = err
	} else {
		r.Error = ex
	}
}

func (r *Result) SetHost(id string) {
	r.host = id
}

func (r *Result) Message() string {
	if r.host == "" {
		r.host, _ = os.Hostname()
	}

	templ := os.Getenv("ERROR_MESSAGE_TEMPLATE")
	if templ == "" {
		templ = "Host: {{.Host}} Exit: {{.Exit}} Error: {{.Error}}"
	}

	t := template.New("errormsg")
	t, _ = t.Parse(templ)
	s := struct{ Host, Exit, Error string }{r.host, r.Exit, r.Error}
	var tpl bytes.Buffer
	t.Execute(&tpl, s)
	return tpl.String()
}
