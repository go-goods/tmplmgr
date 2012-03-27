package tmpl

import (
	"fmt"
	"html/template"
	"io"
	"log"
	"path/filepath"
	"strings"
	"sync"
)

type Mode bool

const (
	Development Mode = true
	Production  Mode = false
)

var compile_mode = Production

func CompileMode(mode Mode) {
	compile_mode = mode
}

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

func (t *Template) Blocks(globs ...string) *Template {
	t.compile_lock.Lock()
	defer t.compile_lock.Unlock()

	t.blocks = append(t.blocks, globs...)
	t.dirty = true
	return t
}

func (t *Template) Call(name string, fnc interface{}) *Template {
	t.compile_lock.Lock()
	defer t.compile_lock.Unlock()

	t.funcs[name] = fnc
	t.dirty = true
	return t
}

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
