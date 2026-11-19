// Package events carries what the services report while they work: log
// lines and the steps of an operation. The CLI prints them on the console;
// a daemon can stream them to its clients.
package events

// Level is the severity of an event.
type Level uint8

// The levels, those of the console log lines.
const (
	Info Level = iota
	Success
	Warning
	Error
)

// Kind tells a log line from a step of an operation.
type Kind uint8

const (
	// Message is a log line: a skipped input, a warning, an error...
	Message Kind = iota
	// Step starts a stage of an operation ("Generating SSL
	// certificates..."), always at the Info level.
	Step
	// Break separates two parts of the output: the console prints a blank
	// line, the other clients may ignore it.
	Break
)

// Event is one thing a service reports.
type Event struct {
	Kind    Kind
	Level   Level
	Message string
}

// Emitter receives the events of a service.
type Emitter interface {
	Emit(Event)
}

// Func is an Emitter function.
type Func func(Event)

// Emit calls f.
func (f Func) Emit(e Event) { f(e) }

// Discard drops every event.
var Discard Emitter = Func(func(Event) {})

// Log reports to an Emitter through the Info, Success, Warning and Error
// methods the core packages take as their logger.
type Log struct {
	e Emitter
}

// NewLog returns a Log emitting to e, dropping the events when e is nil.
func NewLog(e Emitter) Log {
	if e == nil {
		e = Discard
	}
	return Log{e: e}
}

func (l Log) emit(k Kind, level Level, msg string) {
	if l.e == nil {
		return
	}
	l.e.Emit(Event{Kind: k, Level: level, Message: msg})
}

// Info reports an informational message.
func (l Log) Info(msg string) { l.emit(Message, Info, msg) }

// Success reports a success.
func (l Log) Success(msg string) { l.emit(Message, Success, msg) }

// Warning reports a warning.
func (l Log) Warning(msg string) { l.emit(Message, Warning, msg) }

// Error reports an error.
func (l Log) Error(msg string) { l.emit(Message, Error, msg) }

// Step reports the start of a stage of the operation.
func (l Log) Step(msg string) { l.emit(Step, Info, msg) }

// Break separates two parts of the output.
func (l Log) Break() { l.emit(Break, Info, "") }

// ReportedError is the failure of an operation whose error and cleanup
// were already reported as events: the caller must not print it again.
type ReportedError struct {
	Err error
}

func (e *ReportedError) Error() string { return e.Err.Error() }

func (e *ReportedError) Unwrap() error { return e.Err }
