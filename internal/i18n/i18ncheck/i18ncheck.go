// Package i18ncheck checks the translations against the code: every
// translatable literal of the code (the arguments of i18n.T, Tf, Tn,
// Errorf, NewError and N) translated in every catalog, no translation
// left without its message, and the same printf verbs on both sides.
package i18ncheck

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// literalArgs are the positions of the message arguments of each function.
var literalArgs = map[string][]int{
	"T": {0}, "Tf": {0}, "Errorf": {0}, "NewError": {0}, "N": {0},
	"Tn": {1, 2},
}

// Message is a translatable literal of the code.
type Message struct {
	ID  string
	Pos string // file:line of its first use
}

// Scan returns the translatable literals of the Go files under root,
// tests and testdata excluded, and the calls whose message is not a
// literal (only T accepts a variable: text marked with N).
func Scan(root string) (msgs []Message, problems []string, err error) {
	seen := map[string]bool{}
	fset := token.NewFileSet()
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if n := d.Name(); n == "testdata" || n == "vendor" || (strings.HasPrefix(n, ".") && p != root) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		pkg := importName(f)
		if pkg == "" {
			return nil
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if x, ok := sel.X.(*ast.Ident); !ok || x.Name != pkg {
				return true
			}
			positions, ok := literalArgs[sel.Sel.Name]
			if !ok {
				return true
			}
			pos := fset.Position(call.Pos())
			where := fmt.Sprintf("%s:%d", filepath.ToSlash(pos.Filename), pos.Line)
			for _, i := range positions {
				if i >= len(call.Args) {
					continue
				}
				id, ok := literal(call.Args[i])
				if !ok {
					if sel.Sel.Name != "T" {
						problems = append(problems, fmt.Sprintf("%s: the message of i18n.%s must be a string literal", where, sel.Sel.Name))
					}
					continue
				}
				if !seen[id] {
					seen[id] = true
					msgs = append(msgs, Message{ID: id, Pos: where})
				}
			}
			return true
		})
		return nil
	})
	return msgs, problems, err
}

// importName returns the name the file imports the i18n package under,
// "" when it does not.
func importName(f *ast.File) string {
	for _, imp := range f.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		if !strings.HasSuffix(p, "/internal/i18n") {
			continue
		}
		if imp.Name != nil {
			return imp.Name.Name
		}
		return "i18n"
	}
	return ""
}

// literal returns the value of a string literal, or of literals joined
// with + (a long message split over lines).
func literal(e ast.Expr) (string, bool) {
	switch e := e.(type) {
	case *ast.BasicLit:
		if e.Kind != token.STRING {
			return "", false
		}
		s, err := strconv.Unquote(e.Value)
		return s, err == nil
	case *ast.BinaryExpr:
		if e.Op != token.ADD {
			return "", false
		}
		l, ok := literal(e.X)
		if !ok {
			return "", false
		}
		r, ok := literal(e.Y)
		return l + r, ok
	case *ast.ParenExpr:
		return literal(e.X)
	}
	return "", false
}

// Check compares the messages of the code with the catalogs (by language,
// English text to translation) and returns the problems, sorted.
func Check(msgs []Message, catalogs map[string]map[string]string) []string {
	var problems []string
	ids := map[string]bool{}
	for _, m := range msgs {
		ids[m.ID] = true
	}
	for lang, catalog := range catalogs {
		for _, m := range msgs {
			if _, ok := catalog[m.ID]; !ok {
				problems = append(problems, fmt.Sprintf("%s: no %s translation of %q", m.Pos, lang, m.ID))
			}
		}
		for id, text := range catalog {
			switch {
			case !ids[id]:
				problems = append(problems, fmt.Sprintf("locales/%s: %q is no longer in the code", lang, id))
			case strings.TrimSpace(text) == "":
				problems = append(problems, fmt.Sprintf("locales/%s: empty translation of %q", lang, id))
			case !slices.Equal(Verbs(id), Verbs(text)):
				problems = append(problems, fmt.Sprintf("locales/%s: the printf verbs of %q (%v) differ in %q (%v)",
					lang, id, Verbs(id), text, Verbs(text)))
			}
		}
	}
	slices.Sort(problems)
	return problems
}

// Verbs returns the printf verbs of format as "<argument>:<verb>", sorted:
// a translation may reorder its arguments (%[2]s) but must use each one
// with the same verb. "%%" is not a verb.
func Verbs(format string) []string {
	var verbs []string
	arg := 1
	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			continue
		}
		i++
		// flags
		for i < len(format) && strings.IndexByte("+-# 0", format[i]) >= 0 {
			i++
		}
		// argument index, width, precision; * consumes an argument
		for i < len(format) {
			c := format[i]
			switch {
			case c == '[':
				end := strings.IndexByte(format[i:], ']')
				if end < 0 {
					return append(verbs, "bad index")
				}
				n, err := strconv.Atoi(format[i+1 : i+end])
				if err != nil {
					return append(verbs, "bad index")
				}
				arg = n
				i += end + 1
				continue
			case c == '*':
				verbs = append(verbs, strconv.Itoa(arg)+":*")
				arg++
			case c == '.' || (c >= '0' && c <= '9'):
			default:
				goto verb
			}
			i++
		}
	verb:
		if i >= len(format) {
			verbs = append(verbs, "missing verb")
			break
		}
		if format[i] == '%' {
			continue
		}
		verbs = append(verbs, strconv.Itoa(arg)+":"+string(format[i]))
		arg++
	}
	slices.Sort(verbs)
	return verbs
}
