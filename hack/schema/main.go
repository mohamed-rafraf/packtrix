/*
Copyright 2026 The Packtrix Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Command schema writes the JSON Schema of the manifest, kind
// OfflineRepository of packtrix.io/v1alpha1, for editors. It is generated
// from the Go types: their fields, doc comments and defaults, with the
// patterns of the validation and the distros of the built-in catalog as
// examples. Run it
// with make update-schema.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"

	"github.com/packtrix/packtrix/internal/catalog"
	"github.com/packtrix/packtrix/pkg/apis/packtrix"
	"github.com/packtrix/packtrix/pkg/apis/packtrix/v1alpha1"
	"github.com/packtrix/packtrix/pkg/apis/packtrix/validation"
)

// ID is where the schema is published.
const ID = "https://raw.githubusercontent.com/packtrix/packtrix/main/schemas/v1alpha1/offlinerepository.json"

// JSON types.
const (
	typeString = "string"
	typeObject = "object"
)

// Schema is the subset of JSON Schema (draft-07) that the manifest needs.
type Schema struct {
	Schema               string             `json:"$schema,omitempty"`
	ID                   string             `json:"$id,omitempty"`
	Ref                  string             `json:"$ref,omitempty"`
	Title                string             `json:"title,omitempty"`
	Description          string             `json:"description,omitempty"`
	Type                 string             `json:"type,omitempty"`
	Const                string             `json:"const,omitempty"`
	Enum                 []string           `json:"enum,omitempty"`
	Pattern              string             `json:"pattern,omitempty"`
	Examples             []string           `json:"examples,omitempty"`
	MaxLength            int                `json:"maxLength,omitempty"`
	Minimum              *int               `json:"minimum,omitempty"`
	MinItems             int                `json:"minItems,omitempty"`
	Default              any                `json:"default,omitempty"`
	Items                *Schema            `json:"items,omitempty"`
	AllOf                []*Schema          `json:"allOf,omitempty"`
	AnyOf                []*Schema          `json:"anyOf,omitempty"`
	OneOf                []*Schema          `json:"oneOf,omitempty"`
	Required             []string           `json:"required,omitempty"`
	Properties           map[string]*Schema `json:"properties,omitempty"`
	AdditionalProperties any                `json:"additionalProperties,omitempty"`
	Definitions          map[string]*Schema `json:"definitions,omitempty"`
}

func main() {
	var out, src string
	flag.StringVar(&out, "o", "", "write the schema to this file instead of standard output")
	flag.StringVar(&src, "src", "pkg/apis/packtrix/v1alpha1", "directory of the v1alpha1 types, for their doc comments")
	flag.Parse()
	data, err := Generate(src)
	if err == nil {
		if out == "" {
			_, err = os.Stdout.Write(data)
		} else {
			err = os.WriteFile(out, data, 0o644)
		}
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "schema:", err)
		os.Exit(1)
	}
}

// Generate returns the schema as indented JSON. src is the directory of the
// v1alpha1 types.
func Generate(src string) ([]byte, error) {
	docs, err := readDocs(src)
	if err != nil {
		return nil, err
	}
	c, err := catalog.Builtin()
	if err != nil {
		return nil, err
	}
	g := &generator{docs: docs, defaults: defaults(), definitions: map[string]*Schema{}, used: map[string]bool{}}
	for _, d := range c.Distros() {
		g.distros = append(g.distros, d.Name)
	}
	root := &Schema{
		Schema:               "http://json-schema.org/draft-07/schema#",
		ID:                   ID,
		Title:                "Packtrix manifest",
		Description:          docs["OfflineRepository"],
		Type:                 typeObject,
		Required:             []string{"apiVersion", "kind", "metadata", "spec"},
		AdditionalProperties: false,
		Properties: map[string]*Schema{
			"apiVersion": {Const: v1alpha1.SchemeGroupVersion.String()},
			"kind":       {Const: "OfflineRepository"},
			"metadata":   metadata(),
			"spec":       g.of(reflect.TypeFor[v1alpha1.OfflineRepositorySpec]()),
		},
		Definitions: g.definitions,
	}
	if g.err != nil {
		return nil, g.err
	}
	for key := range fields {
		if !g.used[key] {
			return nil, fmt.Errorf("the schema of %s matches no field", key)
		}
	}
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(root); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// metadata is the schema of the object's metadata, of which Packtrix uses
// the name.
func metadata() *Schema {
	labels := &Schema{Type: typeObject, AdditionalProperties: &Schema{Type: typeString}}
	return &Schema{
		Type:                 typeObject,
		Required:             []string{"name"},
		AdditionalProperties: false,
		Properties: map[string]*Schema{
			"name": {
				Description: "The manifest's name, e.g. k8s-nodes.",
				Type:        typeString,
				Pattern:     validation.NamePattern,
				MaxLength:   63,
			},
			"labels":      labels,
			"annotations": labels,
		},
	}
}

// name is the schema of the names of targets and repositories.
var name = &Schema{Type: typeString, Pattern: validation.NamePattern, MaxLength: 63}

// packageName is the schema of package names.
var packageName = &Schema{Type: typeString, Pattern: validation.PackageNamePattern}

func enum(values ...string) *Schema { return &Schema{Type: typeString, Enum: values} }

// fields replace the schema of fields, by type and JSON name, with the
// values that the validation accepts.
var fields = map[string]func(g *generator, s *Schema) *Schema{
	"OfflineRepositorySpec.targets":  minItems,
	"OfflineRepositorySpec.packages": minItems,
	"Target.name":                    func(*generator, *Schema) *Schema { return name },
	"Target.architectures": func(*generator, *Schema) *Schema {
		return &Schema{Type: "array", Items: enum(v1alpha1.SupportedArchitectures...)}
	},
	"Distro.name":     func(g *generator, _ *Schema) *Schema { return g.distro() },
	"Repository.name": func(*generator, *Schema) *Schema { return name },
	"Package.name":    func(*generator, *Schema) *Schema { return packageName },
	"Package.deb":     func(*generator, *Schema) *Schema { return packageName },
	"Package.rpm":     func(*generator, *Schema) *Schema { return packageName },
	"Package.repo":    func(*generator, *Schema) *Schema { return name },
	"Resolve.baseline": func(*generator, *Schema) *Schema {
		return &Schema{AnyOf: []*Schema{
			enum(v1alpha1.BaselineMinimal, v1alpha1.BaselineNone),
			{Type: typeString, Pattern: "^" + v1alpha1.BaselineLockPrefix + ".+"},
		}}
	},
	"Resolve.alternatives": func(*generator, *Schema) *Schema {
		return enum(v1alpha1.AlternativesFirst, v1alpha1.AlternativesAll)
	},
	"Updates.accept": func(*generator, *Schema) *Schema {
		return enum(v1alpha1.AcceptAll, v1alpha1.AcceptSecurity, v1alpha1.AcceptNone)
	},
	"Updates.publish": func(*generator, *Schema) *Schema { return enum(v1alpha1.PublishAuto, v1alpha1.PublishApproval) },
	"Publish.keepSnapshots": func(_ *generator, s *Schema) *Schema {
		s.Minimum = new(0)
		return s
	},
	"Export.format": func(*generator, *Schema) *Schema { return enum(v1alpha1.ExportFormatTarZst) },
}

func minItems(_ *generator, s *Schema) *Schema {
	s.MinItems = 1
	return s
}

type generator struct {
	// docs are the doc comments of types ("Target") and their fields
	// ("Target.Name").
	docs map[string]string
	// defaults are values of struct types with their defaults applied.
	defaults    map[reflect.Type]reflect.Value
	distros     []string
	definitions map[string]*Schema
	// used are the keys of fields that matched.
	used map[string]bool
	err  error
}

// of returns the schema of values of type t.
func (g *generator) of(t reflect.Type) *Schema {
	doc := g.docs[t.Name()]
	switch t {
	case reflect.TypeFor[v1alpha1.Package]():
		return g.define(t, func() *Schema {
			short := annotate(packageName, "A package name, e.g. curl.", nil)
			return &Schema{Description: doc, OneOf: []*Schema{short, g.object(t)}}
		})
	case reflect.TypeFor[v1alpha1.Distro]():
		return g.define(t, func() *Schema {
			short := annotate(g.distro(), "A built-in distribution: packtrix catalog list shows them.", nil)
			return &Schema{Description: doc, OneOf: []*Schema{short, g.object(t)}}
		})
	case reflect.TypeFor[v1alpha1.SecretRef]():
		return &Schema{Description: doc, Type: typeString, Pattern: packtrix.SecretRefPattern}
	case reflect.TypeFor[v1alpha1.RepoType]():
		return enum(string(v1alpha1.RepoTypeAPT), string(v1alpha1.RepoTypeRPM))
	case reflect.TypeFor[v1alpha1.VersionConstraint](), reflect.TypeFor[v1alpha1.Duration]():
		return &Schema{Description: doc, Type: typeString}
	case reflect.TypeFor[v1alpha1.ByteSize]():
		return &Schema{Description: doc, AnyOf: []*Schema{{Type: "integer", Minimum: new(0)}, {Type: typeString}}}
	}
	switch t.Kind() {
	case reflect.Pointer:
		return g.of(t.Elem())
	case reflect.Slice:
		return &Schema{Type: "array", Items: g.of(t.Elem())}
	case reflect.Struct:
		return g.define(t, func() *Schema { return g.object(t) })
	case reflect.String:
		return &Schema{Type: typeString}
	case reflect.Bool:
		return &Schema{Type: "boolean"}
	case reflect.Int:
		return &Schema{Type: "integer"}
	}
	g.fail(fmt.Errorf("type %s has no schema", t))
	return &Schema{}
}

// distro is the schema of a distro's name. Its examples, which editors
// offer, are the catalog's distros; like packtrix validate, it accepts
// others, which render and plan check against the catalog.
func (g *generator) distro() *Schema {
	return &Schema{Type: typeString, Pattern: validation.NamePattern, MaxLength: 63, Examples: g.distros}
}

// define adds the schema of a named type to the definitions, once, and
// returns a reference to it.
func (g *generator) define(t reflect.Type, schema func() *Schema) *Schema {
	if _, ok := g.definitions[t.Name()]; !ok {
		g.definitions[t.Name()] = nil // a type that contains itself refers to itself
		g.definitions[t.Name()] = schema()
	}
	return &Schema{Ref: "#/definitions/" + t.Name()}
}

// object returns the schema of a struct: its fields, with their doc
// comments and defaults. A field without omitempty or omitzero is required.
// The struct's own doc comment goes to its definition.
func (g *generator) object(t reflect.Type) *Schema {
	s := &Schema{Type: typeObject, Properties: map[string]*Schema{}, AdditionalProperties: false}
	if t != reflect.TypeFor[v1alpha1.Package]() && t != reflect.TypeFor[v1alpha1.Distro]() {
		s.Description = g.docs[t.Name()]
	}
	defaults, hasDefaults := g.defaults[t]
	var previous string
	for i := range t.NumField() {
		f := t.Field(i)
		jsonName, opts, _ := strings.Cut(f.Tag.Get("json"), ",")
		if jsonName == "" || jsonName == "-" {
			g.fail(fmt.Errorf("field %s.%s has no JSON name", t.Name(), f.Name))
			continue
		}
		p := g.of(f.Type)
		key := t.Name() + "." + jsonName
		if replace, ok := fields[key]; ok {
			p = replace(g, p)
			g.used[key] = true
		}
		// "Deb and RPM are …" documents both fields.
		doc := g.docs[t.Name()+"."+f.Name]
		if doc == "" && regexp.MustCompile(`\b`+f.Name+`\b`).MatchString(previous) {
			doc = previous
		}
		previous = doc
		var def any
		if hasDefaults && !defaults.Field(i).IsZero() {
			def = jsonValue(defaults.Field(i))
		}
		s.Properties[jsonName] = annotate(p, doc, def)
		if !strings.Contains(opts, "omitempty") && !strings.Contains(opts, "omitzero") {
			s.Required = append(s.Required, jsonName)
		}
	}
	return s
}

// annotate adds a description and a default to a field's schema. A
// reference can't have either in draft-07, so it is wrapped.
func annotate(s *Schema, doc string, def any) *Schema {
	if doc == "" && def == nil {
		return s
	}
	if s.Ref != "" {
		return &Schema{Description: doc, Default: def, AllOf: []*Schema{s}}
	}
	c := *s
	if doc != "" {
		c.Description = doc
	}
	c.Default = def
	return &c
}

func (g *generator) fail(err error) {
	if g.err == nil {
		g.err = err
	}
}

// defaults returns the struct values of an empty manifest and target with
// their defaults applied, by type.
func defaults() map[reflect.Type]reflect.Value {
	m := &v1alpha1.OfflineRepository{}
	v1alpha1.SetObjectDefaults_OfflineRepository(m)
	t := &v1alpha1.Target{}
	v1alpha1.SetDefaults_Target(t)
	out := map[reflect.Type]reflect.Value{}
	var walk func(v reflect.Value)
	walk = func(v reflect.Value) {
		out[v.Type()] = v
		for i := range v.NumField() {
			f := v.Field(i)
			if f.Kind() == reflect.Pointer && !f.IsNil() {
				f = f.Elem()
			}
			if f.Kind() == reflect.Struct {
				walk(f)
			}
		}
	}
	walk(reflect.ValueOf(m.Spec))
	walk(reflect.ValueOf(*t))
	return out
}

// jsonValue returns v as it appears in JSON.
func jsonValue(v reflect.Value) any {
	data, err := json.Marshal(v.Interface())
	if err != nil {
		panic(err)
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		panic(err)
	}
	return out
}

// readDocs returns the doc comments of the types in dir and of their
// fields, each as one line.
func readDocs(dir string) (map[string]string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}
	docs := map[string]string{}
	fset := token.NewFileSet()
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, file, nil, parser.ParseComments)
		if err != nil {
			return nil, err
		}
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.TYPE {
				continue
			}
			for _, spec := range gen.Specs {
				ts := spec.(*ast.TypeSpec)
				doc := ts.Doc
				if doc == nil && len(gen.Specs) == 1 {
					doc = gen.Doc
				}
				docs[ts.Name.Name] = text(doc)
				st, ok := ts.Type.(*ast.StructType)
				if !ok {
					continue
				}
				for _, field := range st.Fields.List {
					for _, n := range field.Names {
						docs[ts.Name.Name+"."+n.Name] = text(field.Doc)
					}
				}
			}
		}
	}
	if len(docs) == 0 {
		return nil, fmt.Errorf("no types in %s", dir)
	}
	return docs, nil
}

func text(c *ast.CommentGroup) string {
	lines := slices.DeleteFunc(strings.Split(c.Text(), "\n"), func(l string) bool {
		return strings.HasPrefix(l, "+") // markers such as +k8s:deepcopy-gen
	})
	return strings.Join(strings.Fields(strings.Join(lines, " ")), " ")
}
