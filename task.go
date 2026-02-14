package goutbox

type Task struct {
	Key string
	Config
	Payload  any
	Error    []error
	Attempts int
}
