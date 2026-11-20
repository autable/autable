package wfruntime

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/dop251/goja"
)

// Libraries are instance-wide modules a bundle imports as "@lib/<name>".
// The templates' esbuild configs turn each such import into a call of the
// global __autable_lib("@lib/<name>") (a CommonJS stub module, so named
// imports work), and that function loads the library's published version
// into the same runtime: once per run, so every importer shares one
// instance, with the same host API (secrets and vars are the calling
// workflow's).
//
// The runtime deliberately defines no global require: bundled npm
// packages that detect Node with `typeof require` keep taking their
// browser paths.

const (
	// LibraryPrefix starts every library import specifier.
	LibraryPrefix = "@lib/"
	// LibraryImportFunction is the global a bundle calls to import a
	// library.
	LibraryImportFunction = "__autable_lib"
)

var (
	libraryNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
	importPattern      = regexp.MustCompile(`\b` + LibraryImportFunction + `\(\s*["']` + regexp.QuoteMeta(LibraryPrefix) + `([^"'\s]*)["']\s*\)`)
)

// Library is a loaded library version, ready to evaluate.
type Library struct {
	Name    string
	Version int64
	Program *goja.Program
}

// LibraryResolver finds the version of a library a run uses: its published
// one. Errors explain why it cannot be loaded (missing, unpublished,
// unsupported SDK) and fail the require call.
type LibraryResolver interface {
	Library(ctx context.Context, name string) (Library, error)
}

// LibraryUse records a library a run loaded.
type LibraryUse struct {
	Name    string `json:"name"`
	Version int64  `json:"version"`
}

// Imports returns the library names a bundle script imports, sorted and
// without duplicates: the arguments of its __autable_lib("@lib/<name>")
// calls. A call with a computed argument is not seen.
func Imports(source string) []string {
	seen := map[string]bool{}
	names := []string{}
	for _, match := range importPattern.FindAllStringSubmatch(source, -1) {
		if name := match[1]; !seen[name] {
			seen[name] = true
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// ValidLibraryName reports whether name can name a library.
func ValidLibraryName(name string) error {
	if !libraryNamePattern.MatchString(name) {
		return fmt.Errorf("library name %q must match %s", name, libraryNamePattern)
	}
	return nil
}

// CompileLibrary compiles a library bundle (dist/lib.js, an IIFE assigning
// __lib) so that evaluating it returns the library's exports without
// leaving a global behind.
func CompileLibrary(name string, version int64, source string) (*goja.Program, error) {
	wrapped := "(function () {" + source + "\n;return typeof __lib === \"undefined\" ? undefined : __lib;\n})()"
	program, err := goja.Compile(fmt.Sprintf("%s%s@%d.js", LibraryPrefix, name, version), wrapped, false)
	if err != nil {
		return nil, fmt.Errorf("compile library %s version %d: %w", name, version, err)
	}
	return program, nil
}

// importLibrary is the global __autable_lib(specifier): it returns the
// exports of the library "@lib/<name>", loading it on first use.
func (machine *vm) importLibrary(call goja.FunctionCall) goja.Value {
	specifier := call.Argument(0).String()
	name, ok := strings.CutPrefix(specifier, LibraryPrefix)
	if !ok || ValidLibraryName(name) != nil {
		machine.throw(fmt.Errorf("%s(%q): libraries are imported as \"@lib/<name>\"", LibraryImportFunction, specifier))
	}
	if exports, ok := machine.libExports[name]; ok {
		return exports
	}
	for index, loading := range machine.libLoading {
		if loading == name {
			cycle := append(append([]string{}, machine.libLoading[index:]...), name)
			machine.throw(fmt.Errorf("library import cycle: %s", strings.Join(cycle, " -> ")))
		}
	}
	if machine.options.Libraries == nil {
		machine.throw(fmt.Errorf("import %q: libraries are not available here", specifier))
	}
	library, err := machine.options.Libraries.Library(machine.ctx, name)
	if err != nil {
		machine.throw(fmt.Errorf("library %s: %w", name, err))
	}
	machine.libLoading = append(machine.libLoading, name)
	value, err := machine.rt.RunProgram(library.Program)
	machine.libLoading = machine.libLoading[:len(machine.libLoading)-1]
	if err != nil {
		var interrupted *goja.InterruptedError
		if errors.As(err, &interrupted) {
			panic(interrupted)
		}
		var exception *goja.Exception
		if errors.As(err, &exception) {
			panic(exception.Value())
		}
		machine.throw(fmt.Errorf("library %s version %d: %w", name, library.Version, err))
	}
	if value == nil || goja.IsUndefined(value) || goja.IsNull(value) {
		machine.throw(fmt.Errorf("library %s version %d does not define __lib (build it as an IIFE with --global-name=__lib)", name, library.Version))
	}
	machine.libExports[name] = value
	machine.libUses = append(machine.libUses, LibraryUse{Name: name, Version: library.Version})
	return value
}
