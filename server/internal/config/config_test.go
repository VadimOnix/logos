package config

import "testing"

func TestAlertDiskPct(t *testing.T) {
	t.Setenv("LOGOS_DATABASE_URL", "postgres://x")

	// Default when unset.
	t.Setenv("LOGOS_ALERT_DISK_PCT", "")
	cfg, err := FromEnv()
	if err != nil || cfg.AlertDiskPct != 90 {
		t.Fatalf("default: pct=%v err=%v", cfg.AlertDiskPct, err)
	}

	// Explicit value, including 0 to disable.
	for _, v := range []struct {
		in   string
		want float64
	}{{"85", 85}, {"0", 0}, {"99.5", 99.5}} {
		t.Setenv("LOGOS_ALERT_DISK_PCT", v.in)
		cfg, err := FromEnv()
		if err != nil || cfg.AlertDiskPct != v.want {
			t.Errorf("%q: pct=%v err=%v", v.in, cfg.AlertDiskPct, err)
		}
	}

	// Out-of-range and non-numeric are rejected.
	for _, bad := range []string{"100", "-1", "high"} {
		t.Setenv("LOGOS_ALERT_DISK_PCT", bad)
		if _, err := FromEnv(); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestAlertTelegram(t *testing.T) {
	t.Setenv("LOGOS_DATABASE_URL", "postgres://x")

	// Both set — accepted and carried through.
	t.Setenv("LOGOS_ALERT_TELEGRAM_TOKEN", "123:abc")
	t.Setenv("LOGOS_ALERT_TELEGRAM_CHAT", "-10042")
	cfg, err := FromEnv()
	if err != nil || cfg.AlertTelegramToken != "123:abc" || cfg.AlertTelegramChat != "-10042" {
		t.Fatalf("both set: cfg=%+v err=%v", cfg, err)
	}

	// One without the other is a config error, not a silent no-op.
	t.Setenv("LOGOS_ALERT_TELEGRAM_CHAT", "")
	if _, err := FromEnv(); err == nil {
		t.Error("token without chat accepted")
	}
	t.Setenv("LOGOS_ALERT_TELEGRAM_TOKEN", "")
	t.Setenv("LOGOS_ALERT_TELEGRAM_CHAT", "-10042")
	if _, err := FromEnv(); err == nil {
		t.Error("chat without token accepted")
	}
}
