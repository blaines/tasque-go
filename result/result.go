package result

import "fmt"
import "os"

type Result struct {
	Exit    string
	Error   string
	Message string
}

func New(exit, err, msg string) Result {
	return Result{exit, err, msg}
}

func (r *Result) SetExit(ex string) {
	r.Exit = ex
	err := os.Getenv(fmt.Sprintf("EXIT%s", ex))
	if err != "" {
		r.Error = err
	} else {
		r.Error = ex
	}
}
