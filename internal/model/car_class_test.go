package model

import (
	"testing"
	"time"
)

func TestCarClassValidate(t *testing.T) {
	tests := []struct {
		name string
		cn   string
		cars []string
		err  error
	}{
		{"ok", " GT3 ", []string{"a", "b"}, nil},
		{"empty name", "  ", []string{"a"}, ErrCarClassNameEmpty},
		{"no cars", "GT3", nil, ErrCarClassNoCars},
		{"empty car", "GT3", []string{"a", " "}, ErrCarClassEmptyCar},
		{"duplicate", "GT3", []string{"a", "b", "a"}, ErrCarClassDuplicateCar},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			class, err := NewCarClass(tc.cn, tc.cars, time.Now())

			if err != tc.err {
				t.Fatalf("expected %v, got %v", tc.err, err)
			}

			if err == nil && class.Name != "GT3" {
				t.Errorf("name not trimmed: %q", class.Name)
			}
		})
	}
}

func TestCheckCarClassNameUnique(t *testing.T) {
	a, _ := NewCarClass("GT3", []string{"x"}, time.Now())
	b, _ := NewCarClass("gt3 ", []string{"y"}, time.Now())
	c, _ := NewCarClass("GT4", []string{"z"}, time.Now())

	if err := CheckCarClassNameUnique(b, []*CarClass{a, c}); err != ErrCarClassNameTaken {
		t.Errorf("expected taken, got %v", err)
	}

	if err := CheckCarClassNameUnique(a, []*CarClass{a, c}); err != nil {
		t.Errorf("class must not collide with itself: %v", err)
	}

	if err := CheckCarClassNameUnique(c, []*CarClass{a, b}); err != nil {
		t.Errorf("unexpected: %v", err)
	}

	if !a.HasCar("x") || a.HasCar("y") {
		t.Error("HasCar")
	}
}
