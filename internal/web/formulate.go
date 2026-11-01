package web

import (
	"bytes"
	"html/template"
	"net/http"
	"reflect"
	"strings"

	"github.com/cj123/formulate"
	"github.com/cj123/formulate/decorators"
	"golang.org/x/net/html"
)

// EncodeFormData uses formulate to build template.HTML from a struct, with its texts in the language of the request.
func EncodeFormData(data interface{}, r *http.Request) (template.HTML, error) {
	return encodeForm(data, formText(r, data), map[string]func() bool{
		"read":   ReadAccess(r),
		"write":  WriteAccess(r),
		"delete": DeleteAccess(r),
		"admin":  AdminAccess(r),
	})
}

func encodeForm(data interface{}, text func(key, english string) string, showConditions map[string]func() bool) (template.HTML, error) {
	buf := new(bytes.Buffer)

	enc := formulate.NewEncoder(buf, &decorator{text: text})
	enc.SetFormat(true)

	for name, condition := range showConditions {
		enc.AddShowCondition(name, formulate.ShowConditionFunc(condition))
	}

	if err := enc.Encode(data); err != nil {
		return "", err
	}

	return template.HTML(buf.String()), nil
}

// FormTexts returns the English texts of the form of data (all fields shown) by message key (form.<type>....), from
// which the English form catalog is generated.
func FormTexts(data interface{}) (map[string]string, error) {
	prefix := "form." + formTypeName(data) + "."
	texts := make(map[string]string)

	record := func(key, english string) string {
		texts[prefix+key] = english

		return english
	}

	all := func() bool { return true }

	if _, err := encodeForm(data, record, map[string]func() bool{"read": all, "write": all, "delete": all, "admin": all}); err != nil {
		return nil, err
	}

	return texts, nil
}

func DecodeFormData(out interface{}, r *http.Request) error {
	if err := r.ParseForm(); err != nil {
		return err
	}

	return formulate.NewDecoder(r.Form).Decode(out)
}

type decorator struct {
	decorators.BootstrapDecorator

	numFieldSets int

	// text translates the texts of the form (see formText), lastKey is the element name of the last label, which the
	// help text that follows it belongs to.
	text    func(key, english string) string
	lastKey string
}

// translate returns the text of the form message key (below form.<type>.), the English text if there is no translation.
func (d *decorator) translate(key, english string) string {
	if d.text == nil || english == "" {
		return english
	}

	return d.text(key, english)
}

// formText returns the translation function for the form of data in the language of the request: the messages are
// form.<type name>.<element name>.label / .help / .option.<value> and form.<type name>.<field name>.fieldset. Texts
// without a translation stay as the struct tags give them, the English catalog is generated from them (see
// TestFormCatalog).
func formText(r *http.Request, data interface{}) func(key, english string) string {
	prefix := "form." + formTypeName(data) + "."

	return func(key, english string) string {
		if msg, ok := lookupForRequest(r, prefix+key); ok {
			return msg
		}

		return english
	}
}

func formTypeName(data interface{}) string {
	t := reflect.TypeOf(data)

	for t != nil && t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	if t == nil {
		return ""
	}

	return t.Name()
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}

	return ""
}

// setText replaces the first text node below n.
func setText(n *html.Node, translate func(english string) string) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if c.Type == html.TextNode {
			c.Data = translate(c.Data)
			return
		}

		if c.FirstChild != nil {
			setText(c, translate)
			return
		}
	}
}

// HelpText trusts the fields helptext to be safe html.
func (d *decorator) HelpText(n *html.Node, field formulate.StructField) {
	n.Data = "div"

	helpText, err := html.Parse(strings.NewReader(d.translate(d.lastKey+".help", field.GetHelpText())))

	if err != nil {
		return
	}

	n.FirstChild = helpText

	d.BootstrapDecorator.HelpText(n, field)
}

func (d *decorator) Label(n *html.Node, field formulate.StructField) {
	if key := attr(n, "for"); key != "" {
		d.lastKey = key
		setText(n, func(english string) string { return d.translate(key+".label", english) })
	}

	d.col3(n)
}

// SelectField translates the texts of the options.
func (d *decorator) SelectField(n *html.Node, field formulate.StructField) {
	key := attr(n, "name")

	for o := n.FirstChild; o != nil; o = o.NextSibling {
		if o.Type == html.ElementNode && o.Data == "option" {
			value := attr(o, "value")
			setText(o, func(english string) string { return d.translate(key+".option."+value, english) })
		}
	}

	d.BootstrapDecorator.SelectField(n, field)
}

func (d *decorator) FieldWrapper(n *html.Node, field formulate.StructField) {
	d.col9(n)
}

func (d *decorator) col3(n *html.Node) {
	formulate.AppendClass(n, "col-sm-3 col-12")
}

func (d *decorator) col9(n *html.Node) {
	formulate.AppendClass(n, "col-sm-9 col-12")
}

func (d *decorator) TextField(n *html.Node, field formulate.StructField) {
	d.BootstrapDecorator.TextField(n, field)

	for _, attr := range n.Attr {
		if attr.Key == "type" && attr.Val == "password" {
			n.Attr = append(n.Attr, html.Attribute{
				Key: "autocomplete",
				Val: "new-password",
			})
		}
	}
}

func (d *decorator) TextareaField(n *html.Node, field formulate.StructField) {
	d.BootstrapDecorator.TextareaField(n, field)

	n.Attr = append(n.Attr, html.Attribute{
		Key: "rows",
		Val: "15",
	})
}

func (d *decorator) Fieldset(n *html.Node, field formulate.StructField) {
	name := d.translate(field.Name+".fieldset", field.GetName())

	if d.numFieldSets == 0 {
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if c.Type == html.ElementNode && c.Data == "legend" {
				setText(c, func(string) string { return name })
			}
		}

		d.BootstrapDecorator.Fieldset(n, field)
	} else {
		parent := n.Parent
		n.Parent.RemoveChild(n)

		card := &html.Node{
			Type: html.ElementNode,
			Data: "div",
		}
		formulate.AppendClass(card, "card", "mt-2", "mb-4")
		parent.AppendChild(card)

		heading := &html.Node{
			Type: html.ElementNode,
			Data: "div",
		}
		formulate.AppendClass(heading, "card-header")

		heading.AppendChild(&html.Node{
			Type: html.TextNode,
			Data: name,
		})

		n.Data = "div"

		for c := n.FirstChild; c != nil; c = c.NextSibling {
			n.RemoveChild(c)
		}

		formulate.AppendClass(n, "card-body")

		card.AppendChild(heading)
		card.AppendChild(n)
	}

	d.numFieldSets++
}
