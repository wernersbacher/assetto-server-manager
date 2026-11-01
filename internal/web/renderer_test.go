package web

import (
	"bytes"
	"fmt"
	"html/template"
	"sync"
	"testing"
)

// Concurrent requests must not see each other's request functions (access, server URLs).
func TestViewTemplateBindsFuncsPerRequest(t *testing.T) {
	proto, err := template.New("page").Funcs(template.FuncMap{
		"ReadAccess": dummyAccessFunc,
		"serverURL":  func(path string) string { return path },
	}).Parse(`{{ define "base" }}{{ ReadAccess }} <a href="{{ serverURL "/x" }}">{{ end }}`)

	if err != nil {
		t.Fatal(err)
	}

	v := &viewTemplate{proto: proto}

	var wg sync.WaitGroup

	errs := make(chan error, 200)

	for i := 0; i < 200; i++ {
		wg.Add(1)

		go func(i int) {
			defer wg.Done()

			access := i%2 == 0
			server := fmt.Sprintf("/x?server=%d", i)

			tmpl, err := v.get()

			if err != nil {
				errs <- err
				return
			}

			defer v.put(tmpl)

			buf := new(bytes.Buffer)

			err = tmpl.Funcs(template.FuncMap{
				"ReadAccess": func() bool { return access },
				"serverURL":  func(string) string { return server },
			}).ExecuteTemplate(buf, "base", nil)

			if err != nil {
				errs <- err
				return
			}

			if want := fmt.Sprintf(`%t <a href="/x?server=%d">`, access, i); buf.String() != want {
				errs <- fmt.Errorf("request %d: got %q, want %q", i, buf.String(), want)
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Error(err)
	}
}
