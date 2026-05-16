package bot

import "testing"

func TestInlineButtonsUseCallbackData(t *testing.T) {
	markup := homeKB()
	for _, row := range markup.InlineKeyboard {
		for _, btn := range row {
			if btn.Unique != "" {
				t.Fatalf("button %q uses Unique instead of callback data", btn.Text)
			}
			if btn.Data == "" {
				t.Fatalf("button %q has empty callback data", btn.Text)
			}
		}
	}
}

func TestNormalizeCallbackDataSupportsOldTelebotUniquePayload(t *testing.T) {
	if got := normalizeCallbackData("draft:start"); got != "draft:start" {
		t.Fatalf("plain callback changed: %q", got)
	}
	if got := normalizeCallbackData("\fdraft:start"); got != "draft:start" {
		t.Fatalf("old callback not normalized: %q", got)
	}
}

func TestHomeMenuDoesNotExposeAdmin(t *testing.T) {
	markup := homeKB()
	for _, row := range markup.InlineKeyboard {
		for _, btn := range row {
			if btn.Text == "Admin" || btn.Data == "menu:admin" {
				t.Fatalf("admin button exposed: %#v", btn)
			}
		}
	}
}

func TestBotCommandsDoNotExposeAdminOrManualScan(t *testing.T) {
	for _, cmd := range botCommands() {
		switch cmd.Text {
		case "users", "allow", "revoke", "test", "status":
			t.Fatalf("obsolete command exposed: %s", cmd.Text)
		}
	}
}
