package model

import "testing"

func TestIsHighRiskCommand(t *testing.T) {
	cases := []struct {
		cmd  string
		want bool
	}{
		{"rm -rf /", true},
		{"rm -rf /var/log", true},
		{"sudo su", true},
		{"sudo su -", true},
		{"iptables -F", true},
		{"mkfs.ext4 /dev/sda1", true},
		{"dd if=/dev/zero of=/dev/sda", true},
		{"chmod -R 777 /", true},
		{"shutdown -h now", true},
		{"reboot", true},
		{"init 0", true},
		// safe commands
		{"ls -la", false},
		{"cat /etc/hosts", false},
		{"echo hello", false},
		{"rm file.txt", false},
		{"chmod 644 file.txt", false},
	}
	for _, tc := range cases {
		got := IsHighRiskCommand(tc.cmd)
		if got != tc.want {
			t.Errorf("IsHighRiskCommand(%q) = %v, want %v", tc.cmd, got, tc.want)
		}
	}
}
