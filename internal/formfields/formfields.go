// Package formfields contains small form helper types shared between configuration structs.
package formfields

import (
	"fmt"
	"net/url"
	"reflect"
	"strconv"

	"golang.org/x/net/html"

	"github.com/cj123/formulate"
)

type FormHeading string

func (f FormHeading) BuildFormElement(_ string, parent *html.Node, field formulate.StructField, _ formulate.Decorator) error {
	// scrap the label
	parent = parent.Parent
	parent.RemoveChild(parent.FirstChild)

	div := &html.Node{
		Type: html.ElementNode,
		Data: "div",
	}

	formulate.AppendClass(div, "col-12")

	div.AppendChild(&html.Node{
		Type: html.ElementNode,
		Data: "hr",
		Attr: []html.Attribute{
			{
				Key: "class",
				Val: "mt-5",
			},
		},
	})

	h2 := &html.Node{
		Type: html.ElementNode,
		Data: "h3",
	}

	h2.AppendChild(&html.Node{
		Type: html.TextNode,
		Data: field.GetName(),
	})

	div.AppendChild(h2)

	parent.AppendChild(div)

	return nil
}

// BoolString is a "true"/"false" string rendered as a checkbox.
type BoolString string

func (b BoolString) BuildFormElement(key string, parent *html.Node, field formulate.StructField, decorator formulate.Decorator) error {
	n := &html.Node{
		Type: html.ElementNode,
		Data: "input",
		Attr: []html.Attribute{
			{
				Key: "type",
				Val: "checkbox",
			},
			{
				Key: "name",
				Val: key,
			},
			{
				Key: "id",
				Val: key,
			},
		},
	}

	checked := b == "true"

	if checked {
		n.Attr = append(n.Attr, html.Attribute{Key: "checked", Val: "checked"})
	}

	parent.AppendChild(n)
	decorator.CheckboxField(n, field)

	return nil
}

func (b BoolString) DecodeFormValue(form url.Values, name string, values []string) (reflect.Value, error) {
	if len(values) == 0 {
		return reflect.Value{}, fmt.Errorf("servermanager: invalid value length, expected 1, got: %d", len(values))
	}

	// the value is either "on" (checkbox) or a number, anything unparsable counts as 0
	if i, err := strconv.ParseInt(values[0], 10, 0); values[0] == "on" || (err == nil && i == 1) {
		return reflect.ValueOf(BoolString("true")), nil
	}

	return reflect.ValueOf(BoolString("false")), nil
}
