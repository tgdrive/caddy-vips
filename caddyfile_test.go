package caddyvips

import (
	"testing"

	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
)

func TestUnmarshalCaddyfileEnableLogs(t *testing.T) {
	var h Handler
	if err := h.UnmarshalCaddyfile(caddyfile.NewTestDispenser(`vips {
		enable_logs on
	}`)); err != nil {
		t.Fatal(err)
	}
	if !h.EnableLogs {
		t.Fatal("enable_logs was not enabled")
	}
}
