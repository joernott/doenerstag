package db

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// docs/11_nonfunctional.md: "Every query uses parameter binding.
// String-concatenated SQL is a review blocker. Identifiers are never taken from
// user input."
//
// A review blocker that only a reviewer can see is one distracted afternoon
// away from being missed, so it is checked here instead. Some queries in this
// package genuinely are assembled -- a PATCH sets only the fields that were
// sent, and the menu filter adds a clause per active filter -- and what makes
// those safe is that assembly only ever produces `$1`, `$2` and fixed SQL text.
// A value never enters the statement.
//
// The two tests below enforce that on the source rather than on a convention,
// and the third keeps the list of reviewed exceptions from going stale.

// The first rule: a SQL string is formatted with nothing but placeholder
// numbers.
//
// Every string literal in this package that looks like SQL is checked, wherever
// it appears, because the SQL-building helpers take their format string as an
// argument: `add("name = $%d", *in.Name)` is where the literal really lives,
// not at the fmt.Sprintf inside the closure. Restricting those literals to %d
// makes the dangerous shape -- %s or %v with a caller's string -- fail here.
func TestNothingButPlaceholderNumbersIsFormattedIntoSQL(t *testing.T) {
	verbs := regexp.MustCompile(`%[^%]`)
	checked := 0

	forEachSourceFile(t, func(_ string, file *ast.File, fset *token.FileSet) {
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			text, ok := unquote(lit)
			if !ok || !looksLikeSQL(text) {
				return true
			}
			checked++

			for _, verb := range verbs.FindAllString(text, -1) {
				if verb == "%d" {
					continue
				}
				t.Errorf("%s: the SQL fragment %q uses %s; only %%d is allowed, "+
					"because anything else can put a value into the statement "+
					"text instead of into a bind parameter",
					position(fset, lit.Pos()), text, verb)
			}
			return true
		})
	})

	if checked == 0 {
		t.Fatal("no SQL fragments were found; the audit would pass vacuously")
	}
	t.Logf("%d SQL fragments checked", checked)
}

// reviewedAssemblies are the functions that build a statement from something
// other than a literal, each with the reason it is safe.
//
// Being on this list is not a pass mark. It means a person read the call sites
// and confirmed that what is concatenated is an identifier this package chose,
// never anything a caller supplied. A function that appears here without a
// reason, or a new function that assembles SQL and is not here, is what the
// test is for.
var reviewedAssemblies = map[string]string{
	"replaceLinks": "the table and column come from the three call sites in this " +
		"file (SetMenuItemTags, SetMenuItemAllergens, SetMenuItemAdditives), " +
		"each passing a string literal. The ids are bound as $1 and $2.",
	"runCleanupStep": "countSQL and deleteSQL are the pair of literals from the " +
		"step table in Run; the predicate's values arrive in args and are bound.",
	"filtersByOwner": "the query is one of the two literals in FiltersByCategory " +
		"and FiltersByMenuItem, each a constant string built from " +
		"availabilityColumns. The restaurant id is bound as $1.",
	"filtersForOne": "the query is one of the two literals in FiltersForCategory " +
		"and FiltersForMenuItem. The owner id is bound as $1.",
	"replaceAttachments": "the table and column come from the two call sites in " +
		"this file (SetCategoryFilters, SetMenuItemFilters), each passing a " +
		"string literal. The ids are bound as $1, $2 and $3.",
}

// The second rule: the SQL handed to the driver is built only from things that
// cannot carry a value.
//
// Every query argument is checked down to its leaves. A string literal, a
// package-level constant holding a column list, a strings.Join of clauses and a
// fmt.Sprintf constrained by the test above are all acceptable. A plain
// variable or a parameter is not -- that is the shape a caller's string would
// arrive in -- unless the enclosing function is on the reviewed list.
//
// strings.Join is trusted rather than followed: what its elements contain
// cannot be known from the syntax. That is precisely why the first test exists.
func TestEveryQueryStringIsBuiltFromConstants(t *testing.T) {
	// The Querier methods that take SQL as their first argument after the
	// context.
	queryMethods := map[string]bool{"Query": true, "QueryRow": true, "Exec": true}

	constants := packageConstants(t)
	queries := 0

	forEachSourceFile(t, func(_ string, file *ast.File, fset *token.FileSet) {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			reviewed := reviewedAssemblies[fn.Name.Name] != ""

			ast.Inspect(fn.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok || !queryMethods[selector.Sel.Name] || len(call.Args) < 2 {
					return true
				}
				queries++

				bad := unsafeLeaf(call.Args[1], constants)
				switch {
				case bad == "":
				case reviewed:
					markReviewUsed(fn.Name.Name)
				default:
					t.Errorf("%s: %s passes SQL built from %s to %s. Values belong "+
						"in bind parameters, never in the statement text. If the "+
						"concatenated part is an identifier this package chose, add "+
						"%s to reviewedAssemblies with the reason.",
						position(fset, call.Args[1].Pos()), fn.Name.Name, bad,
						selector.Sel.Name, fn.Name.Name)
				}
				return true
			})
		}
	})

	if queries == 0 {
		t.Fatal("no queries were found; the audit would pass vacuously")
	}
	t.Logf("%d query sites checked", queries)
}

// usedReview records which reviewed functions actually still assemble SQL.
var usedReview = map[string]bool{}

func markReviewUsed(name string) { usedReview[name] = true }

// An entry that is no longer needed is worse than no entry: it grants a
// permission nobody is using and would quietly cover a future change to that
// function.
func TestNoReviewedAssemblyIsStale(t *testing.T) {
	// The sibling test populates usedReview. Run it first rather than relying
	// on file order.
	t.Run("audit", TestEveryQueryStringIsBuiltFromConstants)

	var stale []string
	for name := range reviewedAssemblies {
		if !usedReview[name] {
			stale = append(stale, name)
		}
	}
	sort.Strings(stale)

	for _, name := range stale {
		t.Errorf("%s is listed in reviewedAssemblies but no longer assembles SQL; "+
			"remove the entry", name)
	}
}

// looksLikeSQL reports whether a string literal is a statement or a fragment of
// one.
//
// Deliberately generous: a false positive costs a %d that was going to be a %d
// anyway, and a false negative is a fragment nobody checked.
func looksLikeSQL(text string) bool {
	if strings.Contains(text, "$%d") || strings.Contains(text, "$1") {
		return true
	}
	upper := strings.ToUpper(text)
	for _, keyword := range []string{
		"SELECT ", "INSERT INTO", "UPDATE ", "DELETE FROM", "WHERE ", " SET ",
		"VALUES ", "RETURNING", "ORDER BY", "JOIN ", " ANY(", "EXISTS (",
	} {
		if strings.Contains(upper, keyword) {
			return true
		}
	}
	return false
}

// unsafeLeaf returns the first part of an expression that could carry a value,
// or "" when every leaf is safe.
func unsafeLeaf(expr ast.Expr, constants map[string]bool) string {
	switch e := expr.(type) {
	case *ast.BasicLit:
		return "" // a literal is the SQL itself

	case *ast.Ident:
		if constants[e.Name] {
			return ""
		}
		return "the variable " + e.Name

	case *ast.SelectorExpr:
		return "the field " + exprName(e.X) + "." + e.Sel.Name

	case *ast.BinaryExpr:
		if bad := unsafeLeaf(e.X, constants); bad != "" {
			return bad
		}
		return unsafeLeaf(e.Y, constants)

	case *ast.ParenExpr:
		return unsafeLeaf(e.X, constants)

	case *ast.CallExpr:
		if isCallTo(e, "fmt", "Sprintf") || isCallTo(e, "strings", "Join") {
			// Both are constrained by the sibling test: Sprintf to %d, and Join
			// to fragments this package assembles from those literals.
			return ""
		}
		if fun, ok := e.Fun.(*ast.SelectorExpr); ok {
			return "the call " + exprName(fun.X) + "." + fun.Sel.Name
		}
		if fun, ok := e.Fun.(*ast.Ident); ok {
			return "the call " + fun.Name
		}
		return "a call"

	default:
		return "an expression that is not a constant"
	}
}

func exprName(e ast.Expr) string {
	if ident, ok := e.(*ast.Ident); ok {
		return ident.Name
	}
	return "?"
}

// packageConstants is every package-level constant and var whose value is
// written out in the source.
//
// Column lists and shared fragments live in these. They are text somebody
// typed, so using one cannot introduce a value.
func packageConstants(t *testing.T) map[string]bool {
	t.Helper()

	names := map[string]bool{}
	empty := map[string]bool{}
	forEachSourceFile(t, func(_ string, file *ast.File, _ *token.FileSet) {
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || (gen.Tok != token.CONST && gen.Tok != token.VAR) {
				continue
			}
			for _, spec := range gen.Specs {
				value, ok := spec.(*ast.ValueSpec)
				if !ok || len(value.Values) == 0 {
					continue
				}
				// Only when every value is a literal or a concatenation of
				// them: a var initialised from a call is not a constant in the
				// sense this test cares about.
				literal := true
				for _, v := range value.Values {
					if unsafeLeaf(v, empty) != "" {
						literal = false
					}
				}
				if !literal {
					continue
				}
				for _, name := range value.Names {
					names[name.Name] = true
				}
			}
		}
	})
	return names
}

// forEachSourceFile parses every non-test .go file in this package.
func forEachSourceFile(t *testing.T, visit func(string, *ast.File, *token.FileSet)) {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading the package directory: %v", err)
	}

	fset := token.NewFileSet()
	seen := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, name, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		seen++
		visit(name, parsed, fset)
	}

	if seen == 0 {
		t.Fatal("no source files were parsed; the audit would pass vacuously")
	}
}

func isCallTo(call *ast.CallExpr, pkg, name string) bool {
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != name || len(call.Args) == 0 {
		return false
	}
	ident, ok := selector.X.(*ast.Ident)
	return ok && ident.Name == pkg
}

func unquote(lit *ast.BasicLit) (string, bool) {
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return "", false
	}
	return value, true
}

func position(fset *token.FileSet, pos token.Pos) string {
	p := fset.Position(pos)
	return filepath.Base(p.Filename) + ":" + strconv.Itoa(p.Line)
}
