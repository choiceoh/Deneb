package phoneops

import (
	"strings"
	"testing"
)

func TestBoundaryParseAlarmTimeMatrix(t *testing.T) {
	tests := []struct {
		raw      string
		hour     int
		minute   int
		wantErr  bool
		errMatch string
	}{
		{raw: "0:0", hour: 0, minute: 0},
		{raw: "00:00", hour: 0, minute: 0},
		{raw: "7:5", hour: 7, minute: 5},
		{raw: "07:05", hour: 7, minute: 5},
		{raw: "12:30", hour: 12, minute: 30},
		{raw: "23:59", hour: 23, minute: 59},
		{raw: "24:00", wantErr: true, errMatch: "out of range"},
		{raw: "23:60", wantErr: true, errMatch: "out of range"},
		{raw: "99:99", wantErr: true, errMatch: "out of range"},
		{raw: "", wantErr: true, errMatch: "24h clock time"},
		{raw: "7", wantErr: true, errMatch: "24h clock time"},
		{raw: "7:", wantErr: true, errMatch: "24h clock time"},
		{raw: ":05", wantErr: true, errMatch: "24h clock time"},
		{raw: "007:05", wantErr: true, errMatch: "24h clock time"},
		{raw: "07:005", wantErr: true, errMatch: "24h clock time"},
		{raw: " 07:05", wantErr: true, errMatch: "24h clock time"},
		{raw: "07:05 ", wantErr: true, errMatch: "24h clock time"},
		{raw: "07:05:00", wantErr: true, errMatch: "24h clock time"},
		{raw: "오전 7:05", wantErr: true, errMatch: "24h clock time"},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			hour, minute, err := parseAlarmTime(tt.raw)
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), tt.errMatch) {
					t.Fatalf("parseAlarmTime(%q) = (%d,%d,%v)", tt.raw, hour, minute, err)
				}
				return
			}
			if err != nil || hour != tt.hour || minute != tt.minute {
				t.Fatalf("parseAlarmTime(%q) = (%d,%d,%v), want (%d,%d,nil)", tt.raw, hour, minute, err, tt.hour, tt.minute)
			}
		})
	}
}

func TestBoundaryParseTimerSecondsMatrix(t *testing.T) {
	tests := []struct {
		raw      string
		seconds  int
		wantErr  bool
		errMatch string
	}{
		{raw: "1s", seconds: 1},
		{raw: "59s", seconds: 59},
		{raw: "60s", seconds: 60},
		{raw: "1m", seconds: 60},
		{raw: "1m30s", seconds: 90},
		{raw: "90s", seconds: 90},
		{raw: "1h", seconds: 3600},
		{raw: "1h30m", seconds: 5400},
		{raw: "23h59m59s", seconds: 86399},
		{raw: "24h", seconds: 86400},
		{raw: "", wantErr: true, errMatch: "explicit unit"},
		{raw: "0", wantErr: true, errMatch: "out of range"},
		{raw: "90", wantErr: true, errMatch: "explicit unit"},
		{raw: "0s", wantErr: true, errMatch: "out of range"},
		{raw: "500ms", wantErr: true, errMatch: "out of range"},
		{raw: "-1s", wantErr: true, errMatch: "out of range"},
		{raw: "24h1s", wantErr: true, errMatch: "out of range"},
		{raw: "25h", wantErr: true, errMatch: "out of range"},
		{raw: "1d", wantErr: true, errMatch: "explicit unit"},
		{raw: " 1m", wantErr: true, errMatch: "explicit unit"},
		{raw: "1m ", wantErr: true, errMatch: "explicit unit"},
	}
	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			seconds, err := parseTimerSeconds(tt.raw)
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), tt.errMatch) {
					t.Fatalf("parseTimerSeconds(%q) = (%d,%v)", tt.raw, seconds, err)
				}
				return
			}
			if err != nil || seconds != tt.seconds {
				t.Fatalf("parseTimerSeconds(%q) = (%d,%v), want (%d,nil)", tt.raw, seconds, err, tt.seconds)
			}
		})
	}
}
