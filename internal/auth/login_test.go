package auth

import (
	"strings"
	"testing"
)

func TestTheCodeIsFoundWhereverThePasteWrapsIt(t *testing.T) {
	final := "authredirect://com.lfp.laligafantasy/?state=s1&code=abc.def"
	for _, pasted := range []string{
		final,
		"  " + final + "\n",
		"curl '" + final + "' \\\n  -H 'accept: text/html' \\\n  -H 'sec-fetch-mode: navigate'",
		`fetch("` + final + `", {"headers": {}})`,
	} {
		code, err := ExtractCode(pasted, "s1")
		if err != nil || code != "abc.def" {
			t.Errorf("%q: %q, %v", pasted, code, err)
		}
	}
}

func TestAnIntermediatePageSaysWhereTheCodeIs(t *testing.T) {
	_, err := ExtractCode("https://accounts.google.com/signin/oauth/v3/consent?authuser=0&client_id=x", "s1")
	if err == nil || !strings.Contains(err.Error(), "accounts.google.com") ||
		!strings.Contains(err.Error(), "canceled") {
		t.Errorf("the Google consent page: %v", err)
	}
}

func TestAnotherLoginsCodeIsRefused(t *testing.T) {
	if _, err := ExtractCode("authredirect://com.lfp.laligafantasy/?state=other&code=abc", "s1"); err == nil {
		t.Error("a code from another login was accepted")
	}
}
