// Package settings reads and changes the configuration (db.sqlite) with
// the metadata of each setting: description, accepted values, how a
// change applies.
package settings

import (
	"strings"

	"github.com/leobsst/frankenphp-cli/internal/core/env"
	"github.com/leobsst/frankenphp-cli/internal/core/validate"
	"github.com/leobsst/frankenphp-cli/internal/i18n"
)

// Masked replaces the value of a secret.
const Masked = "********"

// Entry is a setting and its current value.
type Entry struct {
	Key         string
	Value       string // Masked for a set secret, unless shown
	Default     string
	Description string
	Values      []string // accepted values, nil when free
	Custom      bool     // set, not the default
	Secret      bool
	Apply       env.Apply
}

// List returns every setting of e, the secrets masked unless showSecrets.
func List(e *env.Env, showSecrets bool) []Entry {
	var out []Entry
	for _, d := range env.Definitions() {
		value := e.Get(d.Key)
		if d.Secret && value != "" && !showSecrets {
			value = Masked
		}
		out = append(out, Entry{
			Key: d.Key, Value: value, Default: d.DefaultValue(), Description: d.Describe(),
			Values: d.Values, Custom: e.IsSet(d.Key), Secret: d.Secret, Apply: d.Apply,
		})
	}
	return out
}

// UnknownError is raised for a name that is not a setting.
type UnknownError struct {
	Name  string
	Close []string // the settings whose name is close
}

func (e *UnknownError) Error() string {
	if len(e.Close) > 0 {
		return i18n.Tf("Unknown setting '%s'. Did you mean: %s?", e.Name, strings.Join(e.Close, ", "))
	}
	return i18n.Tf("Unknown setting '%s'. Run 'frankenmanager configuration list' to see them.", e.Name)
}

// Lookup returns the definition of name, an *UnknownError otherwise.
func Lookup(name string) (env.Definition, error) {
	if d, ok := env.Lookup(name); ok {
		return d, nil
	}
	var close []string
	upper := strings.ToUpper(name)
	for _, k := range env.Keys() {
		if strings.Contains(k, upper) || strings.Contains(upper, k) {
			close = append(close, k)
		}
	}
	return env.Definition{}, &UnknownError{Name: name, Close: close}
}

// Check returns the definition of name after checking value (nil for a
// reset), before anything is written.
func Check(name string, value *string) (env.Definition, error) {
	d, err := Lookup(name)
	if err != nil {
		return d, err
	}
	if value == nil {
		return d, nil
	}
	if err := d.Check(*value); err != nil {
		return d, err
	}
	if d.Key == "DEFAULT_PROJECT_PATH" && *value != "" {
		if err := validate.Directory(*value); err != nil {
			return d, err
		}
	}
	return d, nil
}

// Change is what Set did.
type Change struct {
	Definition env.Definition
	Old, New   string
	Shown      string // New, Masked for a secret
}

// Changed reports whether the value changed.
func (c Change) Changed() bool { return c.Old != c.New }

// Set sets a setting checked by Check to value, or back to its default
// (nil). Applying the change (live service, alias, restart) is up to the
// caller, following Definition.Apply.
func Set(e *env.Env, d env.Definition, value *string) (Change, error) {
	old := e.Get(d.Key)
	var err error
	if value != nil {
		err = e.Set(d.Key, *value)
	} else {
		err = e.Reset(d.Key)
	}
	if err != nil {
		return Change{}, err
	}
	now := e.Get(d.Key)
	shown := now
	if d.Secret && shown != "" {
		shown = Masked
	}
	return Change{Definition: d, Old: old, New: now, Shown: shown}, nil
}
