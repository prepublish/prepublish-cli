package tui

import (
	"context"
	"errors"
	"net/mail"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	huh "charm.land/huh/v2"
)

// The prompts. Each one is a single huh field in a one-group form, styled with
// the report's palette, and each one reports an abort the same way: a cancelled
// form is context.Canceled, so a command can treat "the user changed their mind"
// exactly like ctrl+c.

// PromptEmail asks for the address a free audit is attached to. defaultEmail is
// prefilled and accepted as-is, because the second run of a command should not
// ask the same question twice.
func PromptEmail(defaultEmail string) (string, error) {
	value := strings.TrimSpace(defaultEmail)
	field := huh.NewInput().
		Title("Email for this audit").
		Description("Free audits need an address. It is only ever used for the audit.").
		Placeholder("you@example.com").
		Value(&value).
		Validate(validateEmail)
	if err := runForm(field); err != nil {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

// PromptTitle asks for the video title, which is what the audit frames every
// judgment against.
func PromptTitle() (string, error) {
	var value string
	field := huh.NewInput().
		Title("Video title").
		Description("The title the draft is written for. The audit checks the script's promise against it.").
		Placeholder("I Tried 5 Morning Habits for 30 Days").
		Value(&value).
		Validate(required("a title"))
	if err := runForm(field); err != nil {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

// PromptScriptPath asks where the script is. The path is checked here rather
// than after the form closes: an audit that fails on a typo has already cost the
// user a prompt.
func PromptScriptPath() (string, error) {
	var value string
	field := huh.NewInput().
		Title("Script file").
		Description("Path to the draft. Use - to read standard input.").
		Placeholder("script.md").
		Value(&value).
		Validate(func(s string) error {
			s = strings.TrimSpace(s)
			if s == "" {
				return errors.New("a path is required")
			}
			if s == "-" {
				return nil
			}
			info, err := os.Stat(s)
			if err != nil {
				return errors.New("no file at that path")
			}
			if info.IsDir() {
				return errors.New("that is a directory, not a script")
			}
			return nil
		})
	if err := runForm(field); err != nil {
		return "", err
	}
	return strings.TrimSpace(value), nil
}

// Confirm asks a yes/no question, defaulting to no: every destructive or
// billable action in this CLI has to be chosen, never assumed.
func Confirm(q string) (bool, error) {
	value := false
	field := huh.NewConfirm().
		Title(q).
		Affirmative("Yes").
		Negative("No").
		Value(&value)
	if err := runForm(field); err != nil {
		return false, err
	}
	return value, nil
}

// runForm runs one field and normalises its exit: an aborted form (esc, ctrl+c)
// is context.Canceled, and anything else is passed through unchanged.
func runForm(field huh.Field) error {
	form := huh.NewForm(huh.NewGroup(field).WithShowHelp(false)).
		WithTheme(theme()).
		WithShowHelp(false).
		WithShowErrors(true)
	err := form.Run()
	switch {
	case err == nil:
		return nil
	case errors.Is(err, huh.ErrUserAborted):
		return context.Canceled
	case errors.Is(err, huh.ErrTimeout), errors.Is(err, huh.ErrTimeoutUnsupported):
		return err
	case errors.Is(err, tea.ErrInterrupted), errors.Is(err, tea.ErrProgramKilled):
		return context.Canceled
	default:
		return err
	}
}

func required(what string) func(string) error {
	return func(s string) error {
		if strings.TrimSpace(s) == "" {
			return errors.New(what + " is required")
		}
		return nil
	}
}

// validateEmail is deliberately stdlib mail.ParseAddress rather than a regex:
// the server is the authority on what it accepts, and this only has to catch a
// slip before the request is made.
func validateEmail(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return errors.New("an email address is required")
	}
	if _, err := mail.ParseAddress(s); err != nil {
		return errors.New("that does not look like an email address")
	}
	return nil
}
