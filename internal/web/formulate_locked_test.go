package web

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wernersbacher/ACPitwall/internal/acserver/serverconfig"
)

func TestEncodeFormDataLockedDisablesLockedFields(t *testing.T) {
	opts := serverconfig.ConfigIniDefault().GlobalServerConfig
	opts.HTTPPort = 8081
	opts.UDPPort = 9601

	r := httptest.NewRequest("GET", "/server-options", nil)

	form, err := EncodeFormDataLocked(&opts, r, func(field string) bool { return field == "HTTPPort" })

	if err != nil {
		t.Fatal(err)
	}

	inputs := map[string]string{}

	for _, tag := range strings.Split(string(form), "<input") {
		for _, name := range []string{"HTTPPort", "UDPPort"} {
			if strings.Contains(tag, `name="`+name+`"`) || strings.Contains(tag, `.`+name+`"`) {
				inputs[name] = tag[:strings.Index(tag, ">")]
			}
		}
	}

	if tag, ok := inputs["HTTPPort"]; !ok || !strings.Contains(tag, "disabled") || !strings.Contains(tag, "8081") {
		t.Errorf("expected a disabled HTTPPort input with its value, got %q", tag)
	}

	if tag, ok := inputs["UDPPort"]; !ok || strings.Contains(tag, "disabled") {
		t.Errorf("expected an enabled UDPPort input, got %q", tag)
	}

	if got := strings.Count(string(form), "form-locked-hint"); got != 1 {
		t.Errorf("expected one locked hint, got %d", got)
	}

	plain, err := EncodeFormData(&opts, r)

	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(string(plain), "disabled") || strings.Contains(string(plain), "form-locked-hint") {
		t.Error("a form without locks must not contain disabled fields")
	}
}
