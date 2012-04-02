package tmplmgr

import (
	"fmt"
	"html/template"
	"io"
	"log"
	"path/filepath"
	"strings"
	"sync"
)

//Mode is a type that represents one of two modes, Production or Development.
//See CompileMode for details.
type Mode bool

const (
	Development Mode = true
	Production  Mode = false
)

var compile_mode = Production

//CompileMode sets the compilation mode for the package. In Development mode,
//templates read in and compile each file it needs to execute every time it needs
//to execute, always getting the most recent changes. In Production mode, templates
//read and compile each file they need only the first time, caching the results
//for subsequent Execute calls. By default, the package is in Production mode.
func CompileMode(mode Mode) {
	compile_mode = mode
}

//Template is the type that represents a template. It is created by using the
//Parse function and dependencies are attached through Blocks and Call.
type Template struct {
	t *template.Template

	dirty  bool
	base   string
	funcs  template.FuncMap
	blocks []string

	//cached compiled glob sets
	compiled map[string]*template.Template

	compile_lock sync.RWMutex
}

func Parse(file string) *Template {
	return &Template{
		base:     file,
		funcs:    template.FuncMap{},
		compiled: map[string]*template.Template{},
	}
}

//Blocks attaches all of the block definitions in files that match the glob 
//patterns to the template for every Execute call so the base template can
//evoke them.
func (t *Template) Blocks(globs ...string) *Template {
	t.compile_lock.Lock()
	defer t.compile_lock.Unlock()

	t.blocks = append(t.blocks, globs...)
	t.dirty = true
	return t
}

//Call attaches a function to the template under the specified name for every
//Execute call so the base template can call them.
func (t *Template) Call(name string, fnc interface{}) *Template {
	t.compile_lock.Lock()
	defer t.compile_lock.Unlock()

	t.funcs[name] = fnc
	t.dirty = true
	return t
}

//Compile precompiles the template before Execute. Execute will call Compile if
//any Execute level globs are passed in, if the Template has had functions added
//or blocks added since the last Compile, or if the mode is in Development.
func (t *Template) Compile() (err error) {
	t.compile_lock.Lock()
	defer t.compile_lock.Unlock()

	log.Printf("compiling %s %s", t.base, t.blocks)

	tmpl := template.New(filepath.Base(t.base))
	tmpl.Delims(`{%`, `%}`)
	tmpl, err = tmpl.ParseFiles(t.base)
	if err != nil {
		return
	}

	//catch the panic from funcs if theres an invalid func map
	defer func() {
		if e := recover(); e != nil {
			err = fmt.Errorf("%v", e)
		}
	}()
	tmpl.Funcs(t.funcs)

	for _, glob := range t.blocks {
		tmpl, err = tmpl.ParseGlob(glob)
		if err != nil {
			return
		}
	}

	t.t = tmpl
	t.dirty = false
	t.compiled = map[string]*template.Template{}
	return
}

func (t *Template) getCachedGlobs(globs []string) (tmpl *template.Template, err error) {
	key := strings.Join(globs, ",")
	if cached, ex := t.compiled[key]; ex && compile_mode == Production {
		tmpl = cached
		return
	}

	tmpl, _ = t.t.Clone()
	log.Printf("compiling %s", globs)
	for _, glob := range globs {
		tmpl, err = tmpl.ParseGlob(glob)
		if err != nil {
			return
		}
	}

	t.compiled[key] = tmpl
	return
}

//Execute runs the template with the specified context attaching all the block
//definitions in the files that match the given globs sending the output to
//w. Any errors during the compilation of any files that have to be compiled
//(see the discussion on Modes) or during the execution of the template are
//returned.
func (t *Template) Execute(w io.Writer, ctx interface{}, globs ...string) (err error) {
	if t.dirty || compile_mode == Development {
		err = t.Compile()
		if err != nil {
			return
		}
	}

	//grab a read lock to make sure we dont get a half compiled template
	t.compile_lock.RLock()
	defer t.compile_lock.RUnlock()

	var tmpl *template.Template
	if len(globs) > 0 {
		tmpl, err = t.getCachedGlobs(globs)
		if err != nil {
			return
		}
	} else {
		tmpl = t.t
	}

	err = tmpl.Execute(w, ctx)
	return
}
