package desktop

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

func TestEveryDesktopLabelHasEnglishTranslation(t *testing.T) {
	files, e := filepath.Glob("*.go")
	if e != nil {
		t.Fatal(e)
	}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") || path == "language.go" {
			continue
		}
		f, e := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if e != nil {
			t.Fatal(e)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name, ok := call.Fun.(*ast.Ident)
			if !ok || name.Name != "tr" || len(call.Args) != 1 {
				return true
			}
			value, ok := call.Args[0].(*ast.BasicLit)
			if !ok || value.Kind != token.STRING {
				return true
			}
			s, _ := strconv.Unquote(value.Value)
			if strings.IndexFunc(s, func(r rune) bool { return unicode.Is(unicode.Cyrillic, r) }) < 0 {
				return true
			}
			if englishText[s] == "" {
				t.Errorf("%s has untranslated label %q", path, s)
			}
			return true
		})
	}
}
func TestLanguageSwitchDoesNotChangeDataOrLoseRussianLabels(t *testing.T) {
	defer setLanguage("ru")
	setLanguage("en")
	if tr("Система") != "System" || tr("Андрей-PC") != "Андрей-PC" {
		t.Fatal("invalid English translation or user data changed")
	}
	setLanguage("ru")
	if relocalize("Settings") != "Настройки" {
		t.Fatal("Russian switch failed")
	}
}
func TestLightPaletteMatchesSpecification(t *testing.T) {
	p := themePalette(true)
	if p.Background != themeColor(0xD7E8F2) || p.Panel != themeColor(0xFFFFFF) || p.Selection != themeColor(0xB8DDF6) || p.Text != themeColor(0x243F50) {
		t.Fatal("light colors drifted from the approved palette")
	}
}
