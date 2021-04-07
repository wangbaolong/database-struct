package model

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dave/jennifer/jen"
	"github.com/markbates/pkger"
	"gopkg.in/flosch/pongo2.v3"
)

const (
	DbTypeMySQL      = "mysql"
	DbTypePostgreSQL = "postgresql"
)

var (
	ErrTypeNotSupported = errors.New("type not found")

	l = log.New(os.Stdout, "[database-struct] ", log.LstdFlags)
)

type Options struct {
	DbType           string
	Dsn              string
	GenGormTag       bool
	GormV1           bool
	GenJsonTag       bool
	HtmlFile         string
	ModelDir         string
	ModelPackageName string
	ModelSingleFile  bool
	Filters          []*Filter
	Exclude          []string
	Verbose          bool
}

type Filter struct {
	TablePrefix      string
	TableNamePattern string
}

func NewFilter(prefix string, pattern string) *Filter {
	prefix = strings.TrimSpace(prefix)
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return nil
	}
	return &Filter{
		TablePrefix:      prefix,
		TableNamePattern: pattern,
	}
}

type strutter interface {
	dbStruct(*Options) ([]*Table, error)
}

func Generate(options *Options, tables []*Table) error {
	if options.Verbose {
		l.Println("generate table go struct code")
	}

	for _, table := range tables {
		goStruct(options, table)
	}

	if options.HtmlFile != "" {
		tpl := pongo2.Must(pongo2.FromString(pkgerReadString("/template/struct.html")))
		// tpl := pongo2.Must(pongo2.FromFile("template/struct.html"))
		data := pongo2.Context{
			"tables":     tables,
			"tableCount": len(tables),
			"date":       time.Now().Format("2006-01-02 15:04:05"),
			"style": []string{
				pkgerReadString("/assets/style.css"),
				pkgerReadString("/assets/prism/1.20.0/prism.css"),
			},
			"script": []string{
				pkgerReadString("/assets/prism/1.20.0/prism.js"),
			},
		}
		file, err := os.OpenFile(options.HtmlFile, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}

		err = tpl.ExecuteWriter(data, file)
		if err != nil {
			return err
		}
		_ = file.Close()
	}

	if options.ModelDir != "" {
		pkgName := options.ModelPackageName
		if pkgName == "" {
			pkgName = "model"
		}
		headerComment := fmt.Sprintf("code generated by database-struct @%v", time.Now().Format("2006-01-02 15:04:05"))

		if _, err := os.Stat(options.ModelDir); os.IsNotExist(err) {
			err = os.MkdirAll(options.ModelDir, 0700)
			if err != nil {
				return err
			}
		}

		if options.ModelSingleFile {
			f := jen.NewFile(pkgName)
			f.HeaderComment(headerComment)
			for _, table := range tables {
				f.Add(table.goStatement)
				f.Line()
			}
			err := f.Save(filepath.Join(options.ModelDir, "model.go"))
			if err != nil {
				return err
			}
		} else {
			for _, table := range tables {
				f := jen.NewFile(pkgName)
				f.HeaderComment(headerComment)
				f.Add(table.goStatement)
				fileName := fmt.Sprint(strings.TrimPrefix(table.Name, table.Prefix), ".go")
				err := f.Save(filepath.Join(options.ModelDir, fileName))
				if err != nil {
					return err
				}
			}
		}
	}

	if options.Verbose {
		l.Println("Done")
	}

	return nil
}

func DbStruct(options *Options) ([]*Table, error) {
	switch options.DbType {
	case DbTypeMySQL:
		return new(mysql).dbStruct(options)
	case DbTypePostgreSQL:
		return new(postgresql).dbStruct(options)
	}
	return nil, ErrTypeNotSupported
}

func goStruct(options *Options, table *Table) {
	name := TitleCase(strings.TrimPrefix(table.Name, table.Prefix))
	c := jen.
		Commentf("%s table: %s", name, table.Name).Line()

	if table.Comment != "" {
		c = c.Comment(OneLine(table.Comment)).Line()
	}

	c = c.Type().Id(name).Struct(goFields(options, table.Fields)...)

	if table.Prefix != "" {
		c = c.Line().Line().
			Commentf("TableName set table of %v, ref document see https://gorm.io/docs/conventions.html", table.Name).Line().
			Func().Params(jen.Id(name)).Id("TableName").Return(jen.String()).Block(
			jen.Return(jen.Lit(fmt.Sprint(table.Name))),
		)
	}

	table.GoStruct = c.GoString()
	table.goStatement = c
}

func goFields(options *Options, fields []*Field) []jen.Code {
	cs := make([]jen.Code, 0, len(fields))
	for _, f := range fields {
		c := jen.Id(TitleCase(f.Field))
		if f.Nullable {
			c = c.Op("*")
		}
		c = goType(options, f, c)

		tag := make(map[string]string)
		if options.GenGormTag {
			t := fmt.Sprintf(`column:%s;type:%s`, f.Field, f.Type)
			if f.Default != "" {
				t += fmt.Sprint(";default:", f.Default)
			}
			if !f.Nullable {
				t += ";not null"
			}
			if f.Key == "PRI" {
				t += ";primary_key"
			}

			tag["gorm"] = t
		}
		if options.GenJsonTag {
			tag["json"] = CamelCase(f.Field)
		}

		if len(tag) > 0 {
			c.Tag(tag)
		}

		if f.Comment != "" {
			c.Comment(OneLine(f.Comment))
		}

		cs = append(cs, c)
	}

	return cs
}

func goType(options *Options, field *Field, c *jen.Statement) *jen.Statement {
	switch field.GoType {
	case "int":
		return c.Int()
	case "uint":
		return c.Uint()
	case "int8":
		return c.Int8()
	case "uint8":
		return c.Uint8()
	case "int16":
		return c.Int16()
	case "uint16":
		return c.Uint16()
	case "int32":
		return c.Int32()
	case "uint32":
		return c.Uint32()
	case "int64":
		return c.Int64()
	case "uint64":
		return c.Uint64()
	case "string":
		return c.String()
	case "time.Time":
		return c.Qual("time", "Time")
	case "float32":
		return c.Float32()
	case "float64":
		return c.Float64()
	case "[]byte":
		return c.Op("[]").Byte()
	}

	panic(fmt.Sprintf("unknow gotype: %v", field.GoType))
}

func pkgerReadString(filename string) string {
	file, err := pkger.Open(filename)
	if err != nil {
		panic(fmt.Errorf("pkger read string %w", err))
	}
	b, _ := ioutil.ReadAll(file)
	return string(b)
}
