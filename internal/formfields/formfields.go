// Package formfields contains small form helper types shared between configuration structs.
package formfields

import (
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
