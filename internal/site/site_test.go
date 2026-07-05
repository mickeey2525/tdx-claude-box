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

func TestDeriveTDSite(t *testing.T) {
	cases := map[string]string{
		"us01":       "us01",
		"us01-7060":  "us01",
		"ap01-casex": "ap01",
		"eu01":       "eu01",
		"myproject":  "myproject", // 公式 site 形式でなければそのまま
		"us019":      "us019",     // prefix 一致だけでは切らない
	}
	for box, want := range cases {
		if got := DeriveTDSite(box); got != want {
			t.Errorf("DeriveTDSite(%q) = %q, want %q", box, got, want)
		}
	}
}
