package bot

import "testing"

func TestBuildWebAppButton(t *testing.T) {
	button := buildWebAppButton("Buscar", "https://admin.luw17.online/busca")

	webApp, ok := button["web_app"].(map[string]string)
	if !ok {
		t.Fatalf("web_app should be an object with url, got %#v", button["web_app"])
	}
	if got := webApp["url"]; got != "https://admin.luw17.online/busca" {
		t.Fatalf("unexpected url: got %q", got)
	}
}
