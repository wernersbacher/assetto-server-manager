package web_test

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/google/uuid"

	smmodel "github.com/wernersbacher/ACPitwall/internal/model"
	smweb "github.com/wernersbacher/ACPitwall/internal/web"
	"github.com/wernersbacher/ACPitwall/internal/web/webtest"
)

func TestResolveCarClasses(t *testing.T) {
	a := &smmodel.CarClass{ID: uuid.New(), Name: "A", Cars: []string{"x", "y"}}
	b := &smmodel.CarClass{ID: uuid.New(), Name: "B", Cars: []string{"y", "z"}}
	classes := webtest.CarClasses{a, b}

	t.Run("union without duplicates", func(t *testing.T) {
		s, err := smweb.ResolveCarClasses(classes, []string{a.ID.String(), "", b.ID.String(), a.ID.String()})

		if err != nil {
			t.Fatal(err)
		}

		if !reflect.DeepEqual(s.IDs, []string{a.ID.String(), b.ID.String()}) || !reflect.DeepEqual(s.Cars, []string{"x", "y", "z"}) {
			t.Fatalf("%+v", s)
		}

		if got := s.UnionCars([]string{"w", "x"}); !reflect.DeepEqual(got, []string{"w", "x", "y", "z"}) {
			t.Fatalf("union %v", got)
		}
	})

	t.Run("nothing asked for", func(t *testing.T) {
		s, err := smweb.ResolveCarClasses(classes, nil)

		if err != nil || len(s.IDs) != 0 || len(s.UnionCars(nil)) != 0 {
			t.Fatalf("%+v %v", s, err)
		}
	})

	for name, tc := range map[string]struct {
		classes smweb.CarClasses
		ids     []string
	}{
		"unknown":       {classes, []string{uuid.New().String()}},
		"no uuid":       {classes, []string{"../x"}},
		"unknown+known": {classes, []string{a.ID.String(), uuid.New().String()}},
		"no provider":   {nil, []string{a.ID.String()}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := smweb.ResolveCarClasses(tc.classes, tc.ids)

			if !smweb.IsUnknownCarClass(err) {
				t.Fatalf("expected an unknown car class error, got %v", err)
			}

			rec := httptest.NewRecorder()

			if !smweb.FilterError(rec, err) || rec.Code != http.StatusBadRequest {
				t.Fatalf("status %d", rec.Code)
			}
		})
	}

	t.Run("options", func(t *testing.T) {
		options := smweb.CarClassOptions(classes, []string{b.ID.String()})

		if len(options) != 2 || options[0].Selected || !options[1].Selected || options[1].Name != "B" {
			t.Fatalf("%+v", options)
		}

		if smweb.CarClassOptions(nil, nil) != nil {
			t.Fatal("no provider, no options")
		}
	})
}
