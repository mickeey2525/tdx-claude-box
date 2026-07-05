package site

import "testing"

func TestValidate(t *testing.T) {
	valid := []string{"us01", "ap01", "eu01", "dev", "ap01-staging"}
	for _, name := range valid {
		if err := Validate(name); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", name, err)
		}
	}

	invalid := []string{"", "US01", "1ap", "-ap01", "ap 01", "ap01/", "ap01;rm", "あp01",
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"} // 33 chars
	for _, name := range invalid {
		if err := Validate(name); err == nil {
			t.Errorf("Validate(%q) = nil, want error", name)
		}
	}
}
