package cli

import (
	"fmt"
	"io"
	"strings"
)

type Logger struct {
	Out     io.Writer
	Err     io.Writer
	Verbose bool
}

func (l *Logger) Info(format string, args ...any) {
	fmt.Fprintf(l.Out, format+"\n", args...)
}
func (l *Logger) Success(format string, args ...any) {
	fmt.Fprintf(l.Out, "✓ "+format+"\n", args...)
}
func (l *Logger) Warn(format string, args ...any) {
	fmt.Fprintf(l.Out, "! "+format+"\n", args...)
}
func (l *Logger) Error(format string, args ...any) {
	fmt.Fprintf(l.Err, "✗ "+format+"\n", args...)
}
func (l *Logger) Detail(format string, args ...any) {
	if l.Verbose {
		fmt.Fprintf(l.Out, format+"\n", args...)
	}
}
func (l *Logger) Confirm(prompt string, defaultValue bool) bool {
	if !isInteractive() {
		return defaultValue
	}
	suffix := " [Y/n] "
	if !defaultValue {
		suffix = " [y/N] "
	}
	fmt.Fprint(l.Out, prompt+suffix)
	var answer string
	_, _ = fmt.Fscanln(stdinReader, &answer)
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer == "" {
		return defaultValue
	}
	return answer == "y" || answer == "yes"
}
