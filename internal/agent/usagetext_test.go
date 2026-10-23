package agent

import "testing"

func TestParseUsageText(t *testing.T) {
	raw := "\x1b[2J\x1b[1mSettings:\x1b[0m Status  Config  \x1b[7mUsage\x1b[0m\r\n\r\n" +
		" Current session\r\n \x1b[38;5;75m█████████▌\x1b[39m                                         19% used\r\n" +
		" Resets 6pm (Europe/Moscow)\r\n\r\n" +
		" Current week (all models)\r\n ███▌     7% used\r\n Resets Oct 27, 10am (Europe/Moscow)\r\n\r\n" +
		" Current week (Opus)\r\n 1% used\r\n"
	u := ParseUsageText(raw)
	if u == nil || u.FiveHour == nil || u.SevenDay == nil {
		t.Fatalf("%+v", u)
	}
	if u.FiveHour.UsedPercentage != 19 || u.FiveHour.ResetsText != "6pm (Europe/Moscow)" {
		t.Fatalf("session = %+v", u.FiveHour)
	}
	if u.SevenDay.UsedPercentage != 7 || u.SevenDay.ResetsText != "Oct 27, 10am (Europe/Moscow)" {
		t.Fatalf("week = %+v", u.SevenDay)
	}
	if ParseUsageText("nothing here") != nil {
		t.Fatal("expected nil")
	}
}
