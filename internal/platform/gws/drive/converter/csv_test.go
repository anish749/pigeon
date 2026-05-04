package converter

import (
	"testing"
)

func TestToCSVUniformRows(t *testing.T) {
	values := [][]string{
		{"a", "b", "c"},
		{"d", "e", "f"},
	}
	got, err := ToCSV(values)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "a,b,c\nd,e,f\n"
	if string(got) != want {
		t.Errorf("uniform rows: got %q, want %q", string(got), want)
	}
}

func TestToCSVRaggedRows(t *testing.T) {
	values := [][]string{
		{"a", "b", "c"},
		{"d"},
	}
	got, err := ToCSV(values)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "a,b,c\nd,,\n"
	if string(got) != want {
		t.Errorf("ragged rows: got %q, want %q", string(got), want)
	}
}

func TestToCSVEmptyValues(t *testing.T) {
	values := [][]string{
		{"", "", ""},
		{"", "x", ""},
	}
	got, err := ToCSV(values)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := ",,\n,x,\n"
	if string(got) != want {
		t.Errorf("empty values: got %q, want %q", string(got), want)
	}
}

func TestToCSVEmptyInput(t *testing.T) {
	got, err := ToCSV(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("empty input: got %q, want nil", string(got))
	}
}
