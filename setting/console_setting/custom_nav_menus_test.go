package console_setting

import "testing"

func TestValidateCustomNavMenusAcceptsValidItems(t *testing.T) {
	payload := `[
		{
			"id": "docs",
			"title": "Docs",
			"url": "https://example.com/docs",
			"enabled": true,
			"placement": "both",
			"openInNewTab": true,
			"requireAuth": false
		},
		{
			"id": "support",
			"title": "Support",
			"url": "/support",
			"enabled": false,
			"placement": "sidebar",
			"openInNewTab": false,
			"requireAuth": true
		}
	]`

	if err := ValidateConsoleSettings(payload, "CustomNavMenus"); err != nil {
		t.Fatalf("expected valid custom nav menus, got error: %v", err)
	}
}

func TestValidateCustomNavMenusAcceptsLocalhostURL(t *testing.T) {
	payload := `[
		{
			"id": "image-workbench",
			"title": "生图工作台",
			"url": "http://localhost:3030/",
			"enabled": true,
			"placement": "sidebar",
			"openInNewTab": true,
			"requireAuth": true
		}
	]`

	if err := ValidateConsoleSettings(payload, "CustomNavMenus"); err != nil {
		t.Fatalf("expected localhost custom nav menu URL to be valid, got error: %v", err)
	}
}

func TestValidateCustomNavMenusRejectsDuplicateIDs(t *testing.T) {
	payload := `[
		{"id":"docs","title":"Docs","url":"https://example.com/docs","enabled":true,"placement":"top"},
		{"id":"docs","title":"Docs 2","url":"https://example.com/docs2","enabled":true,"placement":"sidebar"}
	]`

	if err := ValidateConsoleSettings(payload, "CustomNavMenus"); err == nil {
		t.Fatal("expected duplicate id to be rejected")
	}
}

func TestValidateCustomNavMenusRejectsUnsafeURL(t *testing.T) {
	payload := `[
		{"id":"bad","title":"Bad","url":"javascript:alert(1)","enabled":true,"placement":"top"}
	]`

	if err := ValidateConsoleSettings(payload, "CustomNavMenus"); err == nil {
		t.Fatal("expected unsafe URL to be rejected")
	}
}

func TestValidateCustomNavMenusRejectsTooManyItems(t *testing.T) {
	payload := `[`
	for i := 0; i < 31; i++ {
		if i > 0 {
			payload += `,`
		}
		payload += `{"id":"item` + string(rune('a'+i%26)) + string(rune('a'+i/26)) + `","title":"Item","url":"/item","enabled":true,"placement":"both"}`
	}
	payload += `]`

	if err := ValidateConsoleSettings(payload, "CustomNavMenus"); err == nil {
		t.Fatal("expected too many custom nav menus to be rejected")
	}
}
