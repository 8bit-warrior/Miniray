package controller

import (
	"strings"
	"testing"
)

func TestPWAHeadInjectionUsesRuntimeBasePath(t *testing.T) {
	tests := []struct {
		name     string
		basePath string
		wantPath string
	}{
		{name: "root", basePath: "/", wantPath: "/manifest.webmanifest"},
		{name: "secret path", basePath: "panel-secret", wantPath: "/panel-secret/manifest.webmanifest"},
		{name: "trailing slash", basePath: "/panel-secret/", wantPath: "/panel-secret/manifest.webmanifest"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			head := string(pwaHeadInjection(test.basePath, "login.html"))
			if !strings.Contains(head, `href="`+test.wantPath+`"`) {
				t.Fatalf("manifest URL = %q, want %q", head, test.wantPath)
			}
			if !strings.Contains(head, `src="`+strings.Replace(test.wantPath, "manifest.webmanifest", "pwa-register.js", 1)+`"`) {
				t.Fatalf("registration URL = %q", head)
			}
		})
	}
}

func TestPWAHeadInjectionSkipsSubscriptionPage(t *testing.T) {
	if got := pwaHeadInjection("/panel-secret/", "subpage.html"); got != nil {
		t.Fatalf("subpage injection = %q, want nil", got)
	}
}
